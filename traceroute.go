package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	traceMaxHops    = 15
	traceTimeout    = 20 * time.Second
	traceGeoTimeout = 4 * time.Second
	traceGeoBaseURL = "https://ipwho.is/"
	traceGeoAltURL  = "https://ipinfo.io/"
)

var (
	traceLinePattern = regexp.MustCompile(`^\s*(\d+)([?:]|\s)\s*(.*)$`)
	traceRTTPattern  = regexp.MustCompile(`(?i)<?([0-9]+(?:\.[0-9]+)?)\s*ms`)
)

// TraceHop is one TTL boundary observed by the platform traceroute command.
// Multiple addresses or RTT values can appear when the path is load-balanced
// or the command sends more than one probe.
type TraceHop struct {
	Hop       int       `json:"hop"`
	Addresses []string  `json:"addresses,omitempty"`
	RTTMS     []float64 `json:"rtt_ms,omitempty"`
	TimedOut  bool      `json:"timed_out"`
	Reached   bool      `json:"reached"`
	Geo       *TraceGeo `json:"geo,omitempty"`
}

// TraceGeo is a best-effort public IP location. Coordinates identify the
// provider's network registration area, not the physical router position.
type TraceGeo struct {
	IP           string  `json:"ip"`
	Hostname     string  `json:"hostname,omitempty"`
	Country      string  `json:"country,omitempty"`
	CountryCode  string  `json:"country_code,omitempty"`
	Region       string  `json:"region,omitempty"`
	City         string  `json:"city,omitempty"`
	Latitude     float64 `json:"latitude"`
	Longitude    float64 `json:"longitude"`
	ASN          int64   `json:"asn,omitempty"`
	Organisation string  `json:"organisation,omitempty"`
	Source       string  `json:"source,omitempty"`
	Confidence   string  `json:"confidence,omitempty"`
}

// TraceResult contains an on-demand route trace. It describes the direct IP
// path from this machine and does not represent hops hidden inside a proxy.
type TraceResult struct {
	Success     bool       `json:"success"`
	Message     string     `json:"message"`
	Error       string     `json:"error,omitempty"`
	TargetHost  string     `json:"target_host"`
	TargetIPs   []string   `json:"target_ips,omitempty"`
	Hops        []TraceHop `json:"hops,omitempty"`
	Reached     bool       `json:"reached"`
	DurationMS  int64      `json:"duration_ms"`
	Tool        string     `json:"tool,omitempty"`
	GeoProvider string     `json:"geo_provider,omitempty"`
	GeoResolved int        `json:"geo_resolved"`
	GeoError    string     `json:"geo_error,omitempty"`
	CheckedAt   string     `json:"checked_at"`
}

// RunTraceroute executes a bounded, on-demand route trace to the target host.
func (a *App) RunTraceroute(targetURL string) TraceResult {
	return a.runTraceroute(a.baseContext(), targetURL)
}

func (a *App) runTraceroute(parent context.Context, targetURL string) TraceResult {
	startedAt := time.Now()
	result := TraceResult{
		Message:   "链路追踪未完成",
		CheckedAt: startedAt.Format(time.RFC3339),
	}

	parsedURL, err := validateHTTPURL(targetURL)
	if err != nil {
		result.Message = "追踪目标配置有误"
		result.Error = err.Error()
		return result
	}
	result.TargetHost = parsedURL.Hostname()

	ctx, cancel := context.WithTimeout(parent, traceTimeout)
	defer cancel()

	if addresses, lookupErr := net.DefaultResolver.LookupIPAddr(ctx, result.TargetHost); lookupErr == nil {
		for _, address := range addresses {
			result.TargetIPs = appendUnique(result.TargetIPs, address.IP.String())
		}
	}

	commandName, args, toolName, err := platformTracerouteCommand(result.TargetHost, traceMaxHops)
	if err != nil {
		result.Message = "当前系统缺少链路追踪工具"
		result.Error = err.Error()
		result.DurationMS = durationMS(time.Since(startedAt))
		return result
	}
	result.Tool = toolName

	output, commandErr := exec.CommandContext(ctx, commandName, args...).CombinedOutput()
	result.Hops = parseTracerouteOutput(string(output), result.TargetIPs)
	result.Reached = traceReached(result.Hops)
	result.Success = len(result.Hops) > 0

	switch {
	case result.Reached:
		result.Message = "已到达目标，链路追踪完成"
	case result.Success && errors.Is(ctx.Err(), context.DeadlineExceeded):
		result.Message = "追踪达到时间上限，已保留可见路径"
	case result.Success:
		result.Message = "已获取部分路径，目标或中间节点可能不响应探测"
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		result.Message = "链路追踪超时，未收到可解析的跳点"
	case commandErr != nil:
		result.Message = "链路追踪命令执行失败"
	default:
		result.Message = "未发现可见的网络跳点"
	}
	if commandErr != nil && !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		result.Error = commandErr.Error()
	}

	if result.Success {
		geoCtx, geoCancel := context.WithTimeout(parent, traceGeoTimeout)
		result.GeoResolved, err = a.enrichTraceGeography(geoCtx, result.Hops)
		geoCancel()
		result.GeoProvider = "ipwho.is + ipinfo.io"
		if err != nil {
			result.GeoError = err.Error()
		}
	}
	result.DurationMS = durationMS(time.Since(startedAt))
	return result
}

func (a *App) enrichTraceGeography(ctx context.Context, hops []TraceHop) (int, error) {
	unique := make(map[string]struct{})
	for _, hop := range hops {
		for _, address := range hop.Addresses {
			if isPublicTraceIP(address) {
				unique[address] = struct{}{}
				break
			}
		}
	}
	if len(unique) == 0 {
		return 0, nil
	}

	type lookupResult struct {
		ip  string
		geo TraceGeo
		err error
	}
	results := make(chan lookupResult, len(unique))
	semaphore := make(chan struct{}, 4)
	client := &http.Client{Timeout: traceGeoTimeout}
	var group sync.WaitGroup
	for address := range unique {
		if cached, ok := a.traceGeoCache.Load(address); ok {
			results <- lookupResult{ip: address, geo: cached.(TraceGeo)}
			continue
		}
		group.Add(1)
		go func(ip string) {
			defer group.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				results <- lookupResult{ip: ip, err: ctx.Err()}
				return
			}
			type geoResult struct {
				geo TraceGeo
				err error
			}
			geoResults := make(chan geoResult, 1)
			hostnames := make(chan string, 1)
			go func() {
				geo, err := lookupTraceGeoConsensus(ctx, client, traceGeoBaseURL, traceGeoAltURL, ip)
				geoResults <- geoResult{geo: geo, err: err}
			}()
			go func() { hostnames <- lookupTraceHostname(ctx, ip) }()
			lookup := <-geoResults
			if hostname := <-hostnames; hostname != "" {
				lookup.geo.Hostname = hostname
			}
			if lookup.err == nil {
				a.traceGeoCache.Store(ip, lookup.geo)
			}
			results <- lookupResult{ip: ip, geo: lookup.geo, err: lookup.err}
		}(address)
	}
	group.Wait()
	close(results)

	locations := make(map[string]TraceGeo, len(unique))
	var firstErr error
	for result := range results {
		if result.err != nil {
			if firstErr == nil {
				firstErr = result.err
			}
			continue
		}
		locations[result.ip] = result.geo
	}

	resolved := 0
	for index := range hops {
		for _, address := range hops[index].Addresses {
			if location, ok := locations[address]; ok {
				copy := location
				hops[index].Geo = &copy
				resolved++
				break
			}
		}
	}
	return resolved, firstErr
}

func lookupTraceGeo(ctx context.Context, client *http.Client, baseURL, address string) (TraceGeo, error) {
	endpoint := strings.TrimRight(baseURL, "/") + "/" + url.PathEscape(address) +
		"?fields=success,message,ip,country,country_code,region,city,latitude,longitude,connection"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return TraceGeo{}, err
	}
	request.Header.Set("User-Agent", "ATNetool/1.0")
	response, err := client.Do(request)
	if err != nil {
		return TraceGeo{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return TraceGeo{}, fmt.Errorf("GeoIP returned HTTP %d", response.StatusCode)
	}
	var payload struct {
		Success     bool    `json:"success"`
		Message     string  `json:"message"`
		IP          string  `json:"ip"`
		Country     string  `json:"country"`
		CountryCode string  `json:"country_code"`
		Region      string  `json:"region"`
		City        string  `json:"city"`
		Latitude    float64 `json:"latitude"`
		Longitude   float64 `json:"longitude"`
		Connection  struct {
			ASN int64  `json:"asn"`
			Org string `json:"org"`
		} `json:"connection"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&payload); err != nil {
		return TraceGeo{}, err
	}
	if !payload.Success {
		return TraceGeo{}, fmt.Errorf("GeoIP lookup failed: %s", payload.Message)
	}
	if payload.Latitude < -90 || payload.Latitude > 90 || payload.Longitude < -180 || payload.Longitude > 180 {
		return TraceGeo{}, errors.New("GeoIP returned invalid coordinates")
	}
	return TraceGeo{
		IP:           payload.IP,
		Country:      payload.Country,
		CountryCode:  payload.CountryCode,
		Region:       payload.Region,
		City:         payload.City,
		Latitude:     payload.Latitude,
		Longitude:    payload.Longitude,
		ASN:          payload.Connection.ASN,
		Organisation: payload.Connection.Org,
		Source:       "ipwho.is",
		Confidence:   "estimated",
	}, nil
}

func lookupTraceGeoAlternative(ctx context.Context, client *http.Client, baseURL, address string) (TraceGeo, error) {
	endpoint := strings.TrimRight(baseURL, "/") + "/" + url.PathEscape(address) + "/json"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return TraceGeo{}, err
	}
	request.Header.Set("User-Agent", "ATNetool/1.0")
	response, err := client.Do(request)
	if err != nil {
		return TraceGeo{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return TraceGeo{}, fmt.Errorf("secondary GeoIP returned HTTP %d", response.StatusCode)
	}
	var payload struct {
		IP      string `json:"ip"`
		City    string `json:"city"`
		Region  string `json:"region"`
		Country string `json:"country"`
		Loc     string `json:"loc"`
		Org     string `json:"org"`
		Bogon   bool   `json:"bogon"`
		Error   string `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&payload); err != nil {
		return TraceGeo{}, err
	}
	if payload.Error != "" || payload.Bogon {
		return TraceGeo{}, fmt.Errorf("secondary GeoIP lookup failed: %s", payload.Error)
	}
	coordinates := strings.SplitN(payload.Loc, ",", 2)
	if len(coordinates) != 2 {
		return TraceGeo{}, errors.New("secondary GeoIP returned no coordinates")
	}
	latitude, latitudeErr := strconv.ParseFloat(coordinates[0], 64)
	longitude, longitudeErr := strconv.ParseFloat(coordinates[1], 64)
	if latitudeErr != nil || longitudeErr != nil || latitude < -90 || latitude > 90 || longitude < -180 || longitude > 180 {
		return TraceGeo{}, errors.New("secondary GeoIP returned invalid coordinates")
	}
	organisation := payload.Org
	if fields := strings.Fields(payload.Org); len(fields) > 1 && strings.HasPrefix(strings.ToUpper(fields[0]), "AS") {
		organisation = strings.Join(fields[1:], " ")
	}
	return TraceGeo{
		IP:           payload.IP,
		Country:      payload.Country,
		CountryCode:  payload.Country,
		Region:       payload.Region,
		City:         payload.City,
		Latitude:     latitude,
		Longitude:    longitude,
		ASN:          parseTraceASN(payload.Org),
		Organisation: organisation,
		Source:       "ipinfo.io",
		Confidence:   "estimated",
	}, nil
}

func lookupTraceGeoConsensus(ctx context.Context, client *http.Client, primaryURL, secondaryURL, address string) (TraceGeo, error) {
	type result struct {
		geo TraceGeo
		err error
	}
	results := make(chan result, 2)
	go func() {
		geo, err := lookupTraceGeo(ctx, client, primaryURL, address)
		results <- result{geo: geo, err: err}
	}()
	go func() {
		geo, err := lookupTraceGeoAlternative(ctx, client, secondaryURL, address)
		results <- result{geo: geo, err: err}
	}()

	available := make([]TraceGeo, 0, 2)
	var lookupErrors []error
	for range 2 {
		lookup := <-results
		if lookup.err != nil {
			lookupErrors = append(lookupErrors, lookup.err)
			continue
		}
		available = append(available, lookup.geo)
	}
	if len(available) == 0 {
		return TraceGeo{}, errors.Join(lookupErrors...)
	}
	if len(available) == 1 {
		return available[0], nil
	}
	return mergeTraceGeo(available[0], available[1]), nil
}

func mergeTraceGeo(primary, secondary TraceGeo) TraceGeo {
	selected, fallback := primary, secondary
	if traceGeoCompleteness(secondary) > traceGeoCompleteness(primary) {
		selected, fallback = secondary, primary
	}
	if selected.IP == "" {
		selected.IP = fallback.IP
	}
	if selected.City == "" {
		selected.City = fallback.City
	}
	if selected.Region == "" {
		selected.Region = fallback.Region
	}
	if selected.Country == "" {
		selected.Country = fallback.Country
	}
	if selected.CountryCode == "" {
		selected.CountryCode = fallback.CountryCode
	}
	if selected.ASN == 0 {
		selected.ASN = fallback.ASN
	}
	if selected.Organisation == "" {
		selected.Organisation = fallback.Organisation
	}
	selected.Source = primary.Source + " + " + secondary.Source

	distance := traceGeoDistanceKM(primary.Latitude, primary.Longitude, secondary.Latitude, secondary.Longitude)
	sameCountry := strings.EqualFold(primary.CountryCode, secondary.CountryCode) && primary.CountryCode != ""
	sameCity := sameCountry && strings.EqualFold(strings.TrimSpace(primary.City), strings.TrimSpace(secondary.City)) && primary.City != ""
	switch {
	case sameCity || distance <= 80:
		selected.Confidence = "high"
	case sameCountry && distance <= 300:
		selected.Confidence = "medium"
	default:
		selected.Confidence = "low"
	}
	return selected
}

func traceGeoCompleteness(geo TraceGeo) int {
	score := 0
	for _, value := range []string{geo.City, geo.Region, geo.CountryCode, geo.Organisation} {
		if strings.TrimSpace(value) != "" {
			score++
		}
	}
	if geo.ASN != 0 {
		score++
	}
	return score
}

func traceGeoDistanceKM(latitudeA, longitudeA, latitudeB, longitudeB float64) float64 {
	const earthRadiusKM = 6371.0
	toRadians := func(value float64) float64 { return value * 3.141592653589793 / 180 }
	latitudeDelta := toRadians(latitudeB - latitudeA)
	longitudeDelta := toRadians(longitudeB - longitudeA)
	latitudeARadians := toRadians(latitudeA)
	latitudeBRadians := toRadians(latitudeB)
	a := 0.5 - math.Cos(latitudeDelta)/2 + math.Cos(latitudeARadians)*math.Cos(latitudeBRadians)*(1-math.Cos(longitudeDelta))/2
	a = math.Max(0, math.Min(1, a))
	return earthRadiusKM * 2 * math.Asin(math.Sqrt(a))
}

func parseTraceASN(value string) int64 {
	value = strings.TrimSpace(strings.TrimPrefix(strings.ToUpper(value), "AS"))
	if fields := strings.Fields(value); len(fields) > 0 {
		value = fields[0]
	}
	asn, _ := strconv.ParseInt(value, 10, 64)
	return asn
}

func lookupTraceHostname(ctx context.Context, address string) string {
	names, err := net.DefaultResolver.LookupAddr(ctx, address)
	if err != nil || len(names) == 0 {
		return ""
	}
	return strings.TrimSuffix(names[0], ".")
}

func isPublicTraceIP(address string) bool {
	ip := net.ParseIP(address)
	return ip != nil && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsUnspecified() &&
		!ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && !ip.IsMulticast()
}

func parseTracerouteOutput(output string, targetIPs []string) []TraceHop {
	targets := make(map[string]struct{}, len(targetIPs))
	for _, address := range targetIPs {
		if parsed := net.ParseIP(address); parsed != nil {
			targets[parsed.String()] = struct{}{}
		}
	}

	hopsByNumber := make(map[int]*TraceHop)
	for _, line := range strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n") {
		matches := traceLinePattern.FindStringSubmatch(line)
		if len(matches) != 4 || matches[2] == "?" {
			continue
		}
		hopNumber, err := strconv.Atoi(matches[1])
		if err != nil || hopNumber <= 0 || hopNumber > traceMaxHops {
			continue
		}

		hop := hopsByNumber[hopNumber]
		if hop == nil {
			hop = &TraceHop{Hop: hopNumber}
			hopsByNumber[hopNumber] = hop
		}
		body := matches[3]
		for _, field := range strings.Fields(body) {
			candidate := strings.Trim(field, "()[]<>,")
			if address := net.ParseIP(candidate); address != nil {
				normalised := address.String()
				hop.Addresses = appendUnique(hop.Addresses, normalised)
				_, reached := targets[normalised]
				hop.Reached = hop.Reached || reached
			}
		}
		for _, rttMatch := range traceRTTPattern.FindAllStringSubmatch(body, -1) {
			if value, parseErr := strconv.ParseFloat(rttMatch[1], 64); parseErr == nil {
				hop.RTTMS = append(hop.RTTMS, value)
			}
		}
	}

	hopNumbers := make([]int, 0, len(hopsByNumber))
	for hopNumber := range hopsByNumber {
		hopNumbers = append(hopNumbers, hopNumber)
	}
	sort.Ints(hopNumbers)
	hops := make([]TraceHop, 0, len(hopNumbers))
	for _, hopNumber := range hopNumbers {
		hop := hopsByNumber[hopNumber]
		hop.TimedOut = len(hop.Addresses) == 0
		hops = append(hops, *hop)
	}
	return hops
}

func traceReached(hops []TraceHop) bool {
	for _, hop := range hops {
		if hop.Reached {
			return true
		}
	}
	return false
}

func tracerouteExecutable(names ...string) (string, error) {
	for _, name := range names {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("none of %s were found", strings.Join(names, ", "))
}
