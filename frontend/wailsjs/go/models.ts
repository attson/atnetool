export namespace main {

	export class BaselineFinding {
	    layer: string;
	    status: string;
	    title: string;
	    detail: string;

	    static createFrom(source: any = {}) {
	        return new BaselineFinding(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.layer = source["layer"];
	        this.status = source["status"];
	        this.title = source["title"];
	        this.detail = source["detail"];
	    }
	}
	export class BaselineRemediation {
	    action: string;
	    title: string;
	    detail: string;

	    static createFrom(source: any = {}) {
	        return new BaselineRemediation(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.action = source["action"];
	        this.title = source["title"];
	        this.detail = source["detail"];
	    }
	}
	export class CDNCandidate {
	    ip: string;
	    source: string;
	    range?: string;

	    static createFrom(source: any = {}) {
	        return new CDNCandidate(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ip = source["ip"];
	        this.source = source["source"];
	        this.range = source["range"];
	    }
	}
	export class CDNGovernanceAdvice {
	    action: string;
	    title: string;
	    detail: string;

	    static createFrom(source: any = {}) {
	        return new CDNGovernanceAdvice(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.action = source["action"];
	        this.title = source["title"];
	        this.detail = source["detail"];
	    }
	}
	export class CDNNodeComparison {
	    ip?: string;
	    source?: string;
	    average_latency_ms: number;
	    loss_rate: number;
	    download_speed_mbps: number;
	    download_tested: boolean;
	    cf_colo?: string;
	    cdn_provider?: string;

	    static createFrom(source: any = {}) {
	        return new CDNNodeComparison(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ip = source["ip"];
	        this.source = source["source"];
	        this.average_latency_ms = source["average_latency_ms"];
	        this.loss_rate = source["loss_rate"];
	        this.download_speed_mbps = source["download_speed_mbps"];
	        this.download_tested = source["download_tested"];
	        this.cf_colo = source["cf_colo"];
	        this.cdn_provider = source["cdn_provider"];
	    }
	}
	export class CDNOptimizeComparison {
	    comparable: boolean;
	    same_ip: boolean;
	    current: CDNNodeComparison;
	    recommended: CDNNodeComparison;
	    latency_delta_ms: number;
	    latency_improvement_percent: number;
	    loss_reduction_points: number;
	    speed_comparable: boolean;
	    speed_delta_mbps: number;
	    speed_improvement_percent: number;
	    speed_multiplier: number;

	    static createFrom(source: any = {}) {
	        return new CDNOptimizeComparison(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.comparable = source["comparable"];
	        this.same_ip = source["same_ip"];
	        this.current = this.convertValues(source["current"], CDNNodeComparison);
	        this.recommended = this.convertValues(source["recommended"], CDNNodeComparison);
	        this.latency_delta_ms = source["latency_delta_ms"];
	        this.latency_improvement_percent = source["latency_improvement_percent"];
	        this.loss_reduction_points = source["loss_reduction_points"];
	        this.speed_comparable = source["speed_comparable"];
	        this.speed_delta_mbps = source["speed_delta_mbps"];
	        this.speed_improvement_percent = source["speed_improvement_percent"];
	        this.speed_multiplier = source["speed_multiplier"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class CDNOptimizeOptions {
	    task_id: string;
	    domain: string;
	    path: string;
	    candidate_ips: string[];
	    ip_ranges: string[];
	    use_preset_ipv4: boolean;
	    use_preset_ipv6: boolean;
	    exclude_current_dns: boolean;
	    test_all_ipv4: boolean;
	    ipv6_samples: number;
	    mode: string;
	    port: number;
	    concurrency: number;
	    samples: number;
	    http_status_codes: number[];
	    colos: string[];
	    min_delay_ms: number;
	    max_delay_ms: number;
	    max_loss_rate: number;
	    download_enabled: boolean;
	    download_url: string;
	    download_count: number;
	    download_seconds: number;
	    min_speed_mbps: number;
	    result_limit: number;
	    debug: boolean;
	    route_mode?: string;
	    upstream_proxy_url?: string;

	    static createFrom(source: any = {}) {
	        return new CDNOptimizeOptions(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.task_id = source["task_id"];
	        this.domain = source["domain"];
	        this.path = source["path"];
	        this.candidate_ips = source["candidate_ips"];
	        this.ip_ranges = source["ip_ranges"];
	        this.use_preset_ipv4 = source["use_preset_ipv4"];
	        this.use_preset_ipv6 = source["use_preset_ipv6"];
	        this.exclude_current_dns = source["exclude_current_dns"];
	        this.test_all_ipv4 = source["test_all_ipv4"];
	        this.ipv6_samples = source["ipv6_samples"];
	        this.mode = source["mode"];
	        this.port = source["port"];
	        this.concurrency = source["concurrency"];
	        this.samples = source["samples"];
	        this.http_status_codes = source["http_status_codes"];
	        this.colos = source["colos"];
	        this.min_delay_ms = source["min_delay_ms"];
	        this.max_delay_ms = source["max_delay_ms"];
	        this.max_loss_rate = source["max_loss_rate"];
	        this.download_enabled = source["download_enabled"];
	        this.download_url = source["download_url"];
	        this.download_count = source["download_count"];
	        this.download_seconds = source["download_seconds"];
	        this.min_speed_mbps = source["min_speed_mbps"];
	        this.result_limit = source["result_limit"];
	        this.debug = source["debug"];
	        this.route_mode = source["route_mode"];
	        this.upstream_proxy_url = source["upstream_proxy_url"];
	    }
	}
	export class ProxyListenerResult {
	    endpoint: string;
	    source: string;
	    listening: boolean;
	    protocol?: string;
	    latency_ms?: number;

	    static createFrom(source: any = {}) {
	        return new ProxyListenerResult(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.endpoint = source["endpoint"];
	        this.source = source["source"];
	        this.listening = source["listening"];
	        this.protocol = source["protocol"];
	        this.latency_ms = source["latency_ms"];
	    }
	}
	export class ProxyCompatibility {
	    detected: boolean;
	    provider: string;
	    mode: string;
	    system_proxy_url?: string;
	    suggested_proxy_url?: string;
	    tunnel_detected: boolean;
	    tunnel_interfaces?: string[];
	    fake_ip_detected: boolean;
	    fake_ip_addresses?: string[];
	    listeners?: ProxyListenerResult[];
	    recommended_route: string;
	    coverage: string;
	    message: string;

	    static createFrom(source: any = {}) {
	        return new ProxyCompatibility(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.detected = source["detected"];
	        this.provider = source["provider"];
	        this.mode = source["mode"];
	        this.system_proxy_url = source["system_proxy_url"];
	        this.suggested_proxy_url = source["suggested_proxy_url"];
	        this.tunnel_detected = source["tunnel_detected"];
	        this.tunnel_interfaces = source["tunnel_interfaces"];
	        this.fake_ip_detected = source["fake_ip_detected"];
	        this.fake_ip_addresses = source["fake_ip_addresses"];
	        this.listeners = this.convertValues(source["listeners"], ProxyListenerResult);
	        this.recommended_route = source["recommended_route"];
	        this.coverage = source["coverage"];
	        this.message = source["message"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class NetworkInterfaceSnapshot {
	    name: string;
	    index: number;
	    mtu: number;
	    hardware?: string;
	    addresses?: string[];
	    address_types?: string[];
	    flags?: string[];
	    type: string;
	    active: boolean;
	    default_route: boolean;
	    virtual: boolean;
	    tunnel: boolean;
	    has_ipv4: boolean;
	    has_ipv6: boolean;
	    mtu_risk: boolean;

	    static createFrom(source: any = {}) {
	        return new NetworkInterfaceSnapshot(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.index = source["index"];
	        this.mtu = source["mtu"];
	        this.hardware = source["hardware"];
	        this.addresses = source["addresses"];
	        this.address_types = source["address_types"];
	        this.flags = source["flags"];
	        this.type = source["type"];
	        this.active = source["active"];
	        this.default_route = source["default_route"];
	        this.virtual = source["virtual"];
	        this.tunnel = source["tunnel"];
	        this.has_ipv4 = source["has_ipv4"];
	        this.has_ipv6 = source["has_ipv6"];
	        this.mtu_risk = source["mtu_risk"];
	    }
	}
	export class NetworkEnvironment {
	    captured_at: string;
	    os: string;
	    arch: string;
	    hostname?: string;
	    dns_servers?: string[];
	    interfaces?: NetworkInterfaceSnapshot[];
	    proxy_mode: string;
	    proxy_url?: string;

	    static createFrom(source: any = {}) {
	        return new NetworkEnvironment(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.captured_at = source["captured_at"];
	        this.os = source["os"];
	        this.arch = source["arch"];
	        this.hostname = source["hostname"];
	        this.dns_servers = source["dns_servers"];
	        this.interfaces = this.convertValues(source["interfaces"], NetworkInterfaceSnapshot);
	        this.proxy_mode = source["proxy_mode"];
	        this.proxy_url = source["proxy_url"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class CDNProbeSample {
	    sequence: number;
	    success: boolean;
	    message: string;
	    failed_stage?: string;
	    error?: string;
	    tcp_connect_ms: number;
	    tls_handshake_ms: number;
	    ttfb_ms: number;
	    total_ms: number;
	    status_code: number;
	    http_protocol?: string;
	    cf_ray?: string;
	    cf_colo?: string;
	    cdn_provider?: string;
	    local_address?: string;
	    remote_address?: string;

	    static createFrom(source: any = {}) {
	        return new CDNProbeSample(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sequence = source["sequence"];
	        this.success = source["success"];
	        this.message = source["message"];
	        this.failed_stage = source["failed_stage"];
	        this.error = source["error"];
	        this.tcp_connect_ms = source["tcp_connect_ms"];
	        this.tls_handshake_ms = source["tls_handshake_ms"];
	        this.ttfb_ms = source["ttfb_ms"];
	        this.total_ms = source["total_ms"];
	        this.status_code = source["status_code"];
	        this.http_protocol = source["http_protocol"];
	        this.cf_ray = source["cf_ray"];
	        this.cf_colo = source["cf_colo"];
	        this.cdn_provider = source["cdn_provider"];
	        this.local_address = source["local_address"];
	        this.remote_address = source["remote_address"];
	    }
	}
	export class CDNProbeResult {
	    index: number;
	    candidate: CDNCandidate;
	    samples: CDNProbeSample[];
	    sample_count: number;
	    success_count: number;
	    success_rate: number;
	    p50_tcp_ms: number;
	    p50_tls_ms: number;
	    p50_ttfb_ms: number;
	    p50_total_ms: number;
	    p95_total_ms: number;
	    jitter_ms: number;
	    status_code: number;
	    cf_ray?: string;
	    cf_colo?: string;
	    verified_cloudflare: boolean;
	    rank?: number;
	    recommended: boolean;
	    sent: number;
	    received: number;
	    loss_rate: number;
	    average_latency_ms: number;
	    download_speed_mbps: number;
	    download_bytes: number;
	    download_duration_ms: number;
	    download_tested: boolean;
	    cdn_provider?: string;
	    eligible: boolean;
	    filter_reason?: string;
	    debug_errors?: string[];
	    local_address?: string;
	    remote_address?: string;

	    static createFrom(source: any = {}) {
	        return new CDNProbeResult(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.index = source["index"];
	        this.candidate = this.convertValues(source["candidate"], CDNCandidate);
	        this.samples = this.convertValues(source["samples"], CDNProbeSample);
	        this.sample_count = source["sample_count"];
	        this.success_count = source["success_count"];
	        this.success_rate = source["success_rate"];
	        this.p50_tcp_ms = source["p50_tcp_ms"];
	        this.p50_tls_ms = source["p50_tls_ms"];
	        this.p50_ttfb_ms = source["p50_ttfb_ms"];
	        this.p50_total_ms = source["p50_total_ms"];
	        this.p95_total_ms = source["p95_total_ms"];
	        this.jitter_ms = source["jitter_ms"];
	        this.status_code = source["status_code"];
	        this.cf_ray = source["cf_ray"];
	        this.cf_colo = source["cf_colo"];
	        this.verified_cloudflare = source["verified_cloudflare"];
	        this.rank = source["rank"];
	        this.recommended = source["recommended"];
	        this.sent = source["sent"];
	        this.received = source["received"];
	        this.loss_rate = source["loss_rate"];
	        this.average_latency_ms = source["average_latency_ms"];
	        this.download_speed_mbps = source["download_speed_mbps"];
	        this.download_bytes = source["download_bytes"];
	        this.download_duration_ms = source["download_duration_ms"];
	        this.download_tested = source["download_tested"];
	        this.cdn_provider = source["cdn_provider"];
	        this.eligible = source["eligible"];
	        this.filter_reason = source["filter_reason"];
	        this.debug_errors = source["debug_errors"];
	        this.local_address = source["local_address"];
	        this.remote_address = source["remote_address"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class PatrolFinding {
	    code: string;
	    level: string;
	    title: string;
	    message: string;

	    static createFrom(source: any = {}) {
	        return new PatrolFinding(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.level = source["level"];
	        this.title = source["title"];
	        this.message = source["message"];
	    }
	}
	export class CDNTargetVerification {
	    status: string;
	    verified: boolean;
	    dns_matched: boolean;
	    header_matched: boolean;
	    matched_ips?: string[];
	    http_status_code?: number;
	    cf_ray?: string;
	    cdn_provider?: string;
	    message: string;
	    verification_error?: string;

	    static createFrom(source: any = {}) {
	        return new CDNTargetVerification(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	        this.verified = source["verified"];
	        this.dns_matched = source["dns_matched"];
	        this.header_matched = source["header_matched"];
	        this.matched_ips = source["matched_ips"];
	        this.http_status_code = source["http_status_code"];
	        this.cf_ray = source["cf_ray"];
	        this.cdn_provider = source["cdn_provider"];
	        this.message = source["message"];
	        this.verification_error = source["verification_error"];
	    }
	}
	export class CDNPathEvidence {
	    mode: string;
	    system_proxy_configured: boolean;
	    system_proxy_url?: string;
	    tunnel_detected: boolean;
	    active_tunnel_interfaces?: string[];
	    hosts_applicable: boolean;
	    message: string;

	    static createFrom(source: any = {}) {
	        return new CDNPathEvidence(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mode = source["mode"];
	        this.system_proxy_configured = source["system_proxy_configured"];
	        this.system_proxy_url = source["system_proxy_url"];
	        this.tunnel_detected = source["tunnel_detected"];
	        this.active_tunnel_interfaces = source["active_tunnel_interfaces"];
	        this.hosts_applicable = source["hosts_applicable"];
	        this.message = source["message"];
	    }
	}
	export class CDNOptimizeResult {
	    schema_version: number;
	    task_id: string;
	    status: string;
	    message: string;
	    domain: string;
	    path: string;
	    target_url: string;
	    started_at: string;
	    finished_at: string;
	    duration_ms: number;
	    dns_ips?: string[];
	    dns_error?: string;
	    candidate_count: number;
	    completed_count: number;
	    verified_count: number;
	    responsive_count: number;
	    eligible_count: number;
	    filtered_count: number;
	    download_tested_count: number;
	    failed_count: number;
	    mode: string;
	    port: number;
	    download_enabled: boolean;
	    download_url?: string;
	    candidate_ranges?: string[];
	    truncated: boolean;
	    warnings?: string[];
	    best_ip?: string;
	    baseline_ip?: string;
	    improvement_percent: number;
	    p95_improvement_percent: number;
	    comparison: CDNOptimizeComparison;
	    path_evidence: CDNPathEvidence;
	    validation_scope: string;
	    target_validated: boolean;
	    target_validation: string;
	    target_verification: CDNTargetVerification;
	    confidence: string;
	    finding: PatrolFinding;
	    governance: CDNGovernanceAdvice[];
	    results: CDNProbeResult[];
	    environment: NetworkEnvironment;
	    requested_route: string;
	    effective_route: string;
	    upstream_proxy_url?: string;
	    proxy_compatibility: ProxyCompatibility;

	    static createFrom(source: any = {}) {
	        return new CDNOptimizeResult(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.schema_version = source["schema_version"];
	        this.task_id = source["task_id"];
	        this.status = source["status"];
	        this.message = source["message"];
	        this.domain = source["domain"];
	        this.path = source["path"];
	        this.target_url = source["target_url"];
	        this.started_at = source["started_at"];
	        this.finished_at = source["finished_at"];
	        this.duration_ms = source["duration_ms"];
	        this.dns_ips = source["dns_ips"];
	        this.dns_error = source["dns_error"];
	        this.candidate_count = source["candidate_count"];
	        this.completed_count = source["completed_count"];
	        this.verified_count = source["verified_count"];
	        this.responsive_count = source["responsive_count"];
	        this.eligible_count = source["eligible_count"];
	        this.filtered_count = source["filtered_count"];
	        this.download_tested_count = source["download_tested_count"];
	        this.failed_count = source["failed_count"];
	        this.mode = source["mode"];
	        this.port = source["port"];
	        this.download_enabled = source["download_enabled"];
	        this.download_url = source["download_url"];
	        this.candidate_ranges = source["candidate_ranges"];
	        this.truncated = source["truncated"];
	        this.warnings = source["warnings"];
	        this.best_ip = source["best_ip"];
	        this.baseline_ip = source["baseline_ip"];
	        this.improvement_percent = source["improvement_percent"];
	        this.p95_improvement_percent = source["p95_improvement_percent"];
	        this.comparison = this.convertValues(source["comparison"], CDNOptimizeComparison);
	        this.path_evidence = this.convertValues(source["path_evidence"], CDNPathEvidence);
	        this.validation_scope = source["validation_scope"];
	        this.target_validated = source["target_validated"];
	        this.target_validation = source["target_validation"];
	        this.target_verification = this.convertValues(source["target_verification"], CDNTargetVerification);
	        this.confidence = source["confidence"];
	        this.finding = this.convertValues(source["finding"], PatrolFinding);
	        this.governance = this.convertValues(source["governance"], CDNGovernanceAdvice);
	        this.results = this.convertValues(source["results"], CDNProbeResult);
	        this.environment = this.convertValues(source["environment"], NetworkEnvironment);
	        this.requested_route = source["requested_route"];
	        this.effective_route = source["effective_route"];
	        this.upstream_proxy_url = source["upstream_proxy_url"];
	        this.proxy_compatibility = this.convertValues(source["proxy_compatibility"], ProxyCompatibility);
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}




	export class CFAccelerationStatus {
	    active: boolean;
	    state: string;
	    domain?: string;
	    preferred_ip?: string;
	    gateway_url?: string;
	    managed_upstream_url?: string;
	    fallback_proxy_url?: string;
	    effective_route?: string;
	    started_at?: string;
	    system_proxy_applied: boolean;
	    can_restore: boolean;
	    request_count: number;
	    recovered: boolean;
	    message: string;

	    static createFrom(source: any = {}) {
	        return new CFAccelerationStatus(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.active = source["active"];
	        this.state = source["state"];
	        this.domain = source["domain"];
	        this.preferred_ip = source["preferred_ip"];
	        this.gateway_url = source["gateway_url"];
	        this.managed_upstream_url = source["managed_upstream_url"];
	        this.fallback_proxy_url = source["fallback_proxy_url"];
	        this.effective_route = source["effective_route"];
	        this.started_at = source["started_at"];
	        this.system_proxy_applied = source["system_proxy_applied"];
	        this.can_restore = source["can_restore"];
	        this.request_count = source["request_count"];
	        this.recovered = source["recovered"];
	        this.message = source["message"];
	    }
	}
	export class DNSComparison {
	    successful_resolvers: number;
	    matching_resolvers: number;
	    different: boolean;

	    static createFrom(source: any = {}) {
	        return new DNSComparison(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.successful_resolvers = source["successful_resolvers"];
	        this.matching_resolvers = source["matching_resolvers"];
	        this.different = source["different"];
	    }
	}
	export class DNSDeepComparison {
	    reachable_resolvers: number;
	    public_resolvers: number;
	    system_available: boolean;
	    udp_available: boolean;
	    doh_available: boolean;
	    system_matches_public: boolean;
	    public_address_sets: number;
	    address_difference: boolean;
	    private_rewrite_risk: boolean;
	    dnssec_observed: boolean;

	    static createFrom(source: any = {}) {
	        return new DNSDeepComparison(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.reachable_resolvers = source["reachable_resolvers"];
	        this.public_resolvers = source["public_resolvers"];
	        this.system_available = source["system_available"];
	        this.udp_available = source["udp_available"];
	        this.doh_available = source["doh_available"];
	        this.system_matches_public = source["system_matches_public"];
	        this.public_address_sets = source["public_address_sets"];
	        this.address_difference = source["address_difference"];
	        this.private_rewrite_risk = source["private_rewrite_risk"];
	        this.dnssec_observed = source["dnssec_observed"];
	    }
	}
	export class DNSDiagnoseOptions {
	    task_id: string;
	    domain: string;

	    static createFrom(source: any = {}) {
	        return new DNSDiagnoseOptions(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.task_id = source["task_id"];
	        this.domain = source["domain"];
	    }
	}
	export class DNSRemediation {
	    action: string;
	    title: string;
	    detail: string;

	    static createFrom(source: any = {}) {
	        return new DNSRemediation(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.action = source["action"];
	        this.title = source["title"];
	        this.detail = source["detail"];
	    }
	}
	export class DNSFinding {
	    code: string;
	    layer: string;
	    status: string;
	    title: string;
	    detail: string;

	    static createFrom(source: any = {}) {
	        return new DNSFinding(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.layer = source["layer"];
	        this.status = source["status"];
	        this.title = source["title"];
	        this.detail = source["detail"];
	    }
	}
	export class DNSRecord {
	    type: string;
	    name: string;
	    value: string;
	    ttl: number;

	    static createFrom(source: any = {}) {
	        return new DNSRecord(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.type = source["type"];
	        this.name = source["name"];
	        this.value = source["value"];
	        this.ttl = source["ttl"];
	    }
	}
	export class DNSQueryResult {
	    type: string;
	    responded: boolean;
	    success: boolean;
	    rcode?: string;
	    duration_ms: number;
	    authentic_data: boolean;
	    truncated: boolean;
	    records?: DNSRecord[];
	    error?: string;

	    static createFrom(source: any = {}) {
	        return new DNSQueryResult(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.type = source["type"];
	        this.responded = source["responded"];
	        this.success = source["success"];
	        this.rcode = source["rcode"];
	        this.duration_ms = source["duration_ms"];
	        this.authentic_data = source["authentic_data"];
	        this.truncated = source["truncated"];
	        this.records = this.convertValues(source["records"], DNSRecord);
	        this.error = source["error"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class DNSResolverResult {
	    id: string;
	    name: string;
	    transport: string;
	    endpoint?: string;
	    reachable: boolean;
	    duration_ms: number;
	    queries: DNSQueryResult[];

	    static createFrom(source: any = {}) {
	        return new DNSResolverResult(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.transport = source["transport"];
	        this.endpoint = source["endpoint"];
	        this.reachable = source["reachable"];
	        this.duration_ms = source["duration_ms"];
	        this.queries = this.convertValues(source["queries"], DNSQueryResult);
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class DNSDiagnoseResult {
	    schema_version: number;
	    task_id: string;
	    status: string;
	    message: string;
	    domain: string;
	    started_at: string;
	    finished_at: string;
	    duration_ms: number;
	    completed: number;
	    total: number;
	    resolvers: DNSResolverResult[];
	    comparison: DNSDeepComparison;
	    findings: DNSFinding[];
	    remediations: DNSRemediation[];
	    environment: NetworkEnvironment;

	    static createFrom(source: any = {}) {
	        return new DNSDiagnoseResult(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.schema_version = source["schema_version"];
	        this.task_id = source["task_id"];
	        this.status = source["status"];
	        this.message = source["message"];
	        this.domain = source["domain"];
	        this.started_at = source["started_at"];
	        this.finished_at = source["finished_at"];
	        this.duration_ms = source["duration_ms"];
	        this.completed = source["completed"];
	        this.total = source["total"];
	        this.resolvers = this.convertValues(source["resolvers"], DNSResolverResult);
	        this.comparison = this.convertValues(source["comparison"], DNSDeepComparison);
	        this.findings = this.convertValues(source["findings"], DNSFinding);
	        this.remediations = this.convertValues(source["remediations"], DNSRemediation);
	        this.environment = this.convertValues(source["environment"], NetworkEnvironment);
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

	export class DNSProbeResult {
	    name: string;
	    server?: string;
	    success: boolean;
	    duration_ms: number;
	    ipv4?: string[];
	    ipv6?: string[];
	    error?: string;

	    static createFrom(source: any = {}) {
	        return new DNSProbeResult(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.server = source["server"];
	        this.success = source["success"];
	        this.duration_ms = source["duration_ms"];
	        this.ipv4 = source["ipv4"];
	        this.ipv6 = source["ipv6"];
	        this.error = source["error"];
	    }
	}




	export class DefaultRoute {
	    family: string;
	    interface?: string;
	    gateway?: string;
	    local_ip?: string;
	    metric?: number;
	    selected: boolean;

	    static createFrom(source: any = {}) {
	        return new DefaultRoute(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.family = source["family"];
	        this.interface = source["interface"];
	        this.gateway = source["gateway"];
	        this.local_ip = source["local_ip"];
	        this.metric = source["metric"];
	        this.selected = source["selected"];
	    }
	}
	export class DiagOptions {
	    target_url: string;
	    proxy_mode: string;
	    proxy_url?: string;

	    static createFrom(source: any = {}) {
	        return new DiagOptions(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.target_url = source["target_url"];
	        this.proxy_mode = source["proxy_mode"];
	        this.proxy_url = source["proxy_url"];
	    }
	}
	export class DiagResult {
	    success: boolean;
	    message: string;
	    failed_stage?: string;
	    error?: string;
	    target_url: string;
	    final_url?: string;
	    dns_lookup_ms: number;
	    dns_hosts?: string[];
	    tcp_connect_ms: number;
	    tls_handshake_ms: number;
	    ttfb_ms: number;
	    total_ms: number;
	    http_protocol?: string;
	    status_code: number;
	    redirect_count: number;
	    redirect_chain?: string[];
	    server_header?: string;
	    content_type?: string;
	    content_length: number;
	    response_bytes: number;
	    response_truncated: boolean;
	    cf_ray?: string;
	    cf_colo?: string;
	    cf_cache_status?: string;
	    client_ip?: string;
	    remote_ip?: string;
	    local_address?: string;
	    remote_address?: string;
	    resolved_ips?: string[];
	    tls_version?: string;
	    tls_cipher_suite?: string;
	    tls_alpn?: string;
	    tls_cert_subject?: string;
	    tls_cert_issuer?: string;
	    tls_cert_not_before?: string;
	    tls_cert_expires_at?: string;
	    tls_cert_days_remaining: number;
	    tls_cert_valid: boolean;
	    checked_at: string;
	    proxy_mode: string;
	    proxy_status: string;
	    proxy_detected: boolean;
	    proxy_url?: string;
	    proxy_message: string;

	    static createFrom(source: any = {}) {
	        return new DiagResult(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.message = source["message"];
	        this.failed_stage = source["failed_stage"];
	        this.error = source["error"];
	        this.target_url = source["target_url"];
	        this.final_url = source["final_url"];
	        this.dns_lookup_ms = source["dns_lookup_ms"];
	        this.dns_hosts = source["dns_hosts"];
	        this.tcp_connect_ms = source["tcp_connect_ms"];
	        this.tls_handshake_ms = source["tls_handshake_ms"];
	        this.ttfb_ms = source["ttfb_ms"];
	        this.total_ms = source["total_ms"];
	        this.http_protocol = source["http_protocol"];
	        this.status_code = source["status_code"];
	        this.redirect_count = source["redirect_count"];
	        this.redirect_chain = source["redirect_chain"];
	        this.server_header = source["server_header"];
	        this.content_type = source["content_type"];
	        this.content_length = source["content_length"];
	        this.response_bytes = source["response_bytes"];
	        this.response_truncated = source["response_truncated"];
	        this.cf_ray = source["cf_ray"];
	        this.cf_colo = source["cf_colo"];
	        this.cf_cache_status = source["cf_cache_status"];
	        this.client_ip = source["client_ip"];
	        this.remote_ip = source["remote_ip"];
	        this.local_address = source["local_address"];
	        this.remote_address = source["remote_address"];
	        this.resolved_ips = source["resolved_ips"];
	        this.tls_version = source["tls_version"];
	        this.tls_cipher_suite = source["tls_cipher_suite"];
	        this.tls_alpn = source["tls_alpn"];
	        this.tls_cert_subject = source["tls_cert_subject"];
	        this.tls_cert_issuer = source["tls_cert_issuer"];
	        this.tls_cert_not_before = source["tls_cert_not_before"];
	        this.tls_cert_expires_at = source["tls_cert_expires_at"];
	        this.tls_cert_days_remaining = source["tls_cert_days_remaining"];
	        this.tls_cert_valid = source["tls_cert_valid"];
	        this.checked_at = source["checked_at"];
	        this.proxy_mode = source["proxy_mode"];
	        this.proxy_status = source["proxy_status"];
	        this.proxy_detected = source["proxy_detected"];
	        this.proxy_url = source["proxy_url"];
	        this.proxy_message = source["proxy_message"];
	    }
	}
	export class DiagnosisTaskOptions {
	    task_id: string;
	    target_url: string;
	    proxy_mode: string;
	    proxy_url?: string;

	    static createFrom(source: any = {}) {
	        return new DiagnosisTaskOptions(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.task_id = source["task_id"];
	        this.target_url = source["target_url"];
	        this.proxy_mode = source["proxy_mode"];
	        this.proxy_url = source["proxy_url"];
	    }
	}
	export class TraceGeo {
	    ip: string;
	    hostname?: string;
	    country?: string;
	    country_code?: string;
	    region?: string;
	    city?: string;
	    latitude: number;
	    longitude: number;
	    asn?: number;
	    organisation?: string;
	    source?: string;
	    confidence?: string;

	    static createFrom(source: any = {}) {
	        return new TraceGeo(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ip = source["ip"];
	        this.hostname = source["hostname"];
	        this.country = source["country"];
	        this.country_code = source["country_code"];
	        this.region = source["region"];
	        this.city = source["city"];
	        this.latitude = source["latitude"];
	        this.longitude = source["longitude"];
	        this.asn = source["asn"];
	        this.organisation = source["organisation"];
	        this.source = source["source"];
	        this.confidence = source["confidence"];
	    }
	}
	export class TraceHop {
	    hop: number;
	    addresses?: string[];
	    rtt_ms?: number[];
	    timed_out: boolean;
	    reached: boolean;
	    geo?: TraceGeo;

	    static createFrom(source: any = {}) {
	        return new TraceHop(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hop = source["hop"];
	        this.addresses = source["addresses"];
	        this.rtt_ms = source["rtt_ms"];
	        this.timed_out = source["timed_out"];
	        this.reached = source["reached"];
	        this.geo = this.convertValues(source["geo"], TraceGeo);
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class TraceResult {
	    success: boolean;
	    message: string;
	    error?: string;
	    target_host: string;
	    target_ips?: string[];
	    hops?: TraceHop[];
	    reached: boolean;
	    duration_ms: number;
	    tool?: string;
	    geo_provider?: string;
	    geo_resolved: number;
	    geo_error?: string;
	    checked_at: string;

	    static createFrom(source: any = {}) {
	        return new TraceResult(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.message = source["message"];
	        this.error = source["error"];
	        this.target_host = source["target_host"];
	        this.target_ips = source["target_ips"];
	        this.hops = this.convertValues(source["hops"], TraceHop);
	        this.reached = source["reached"];
	        this.duration_ms = source["duration_ms"];
	        this.tool = source["tool"];
	        this.geo_provider = source["geo_provider"];
	        this.geo_resolved = source["geo_resolved"];
	        this.geo_error = source["geo_error"];
	        this.checked_at = source["checked_at"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class DiagnosisTaskResult {
	    schema_version: number;
	    task_id: string;
	    status: string;
	    started_at: string;
	    finished_at: string;
	    diagnosis: DiagResult;
	    traceroute: TraceResult;
	    environment: NetworkEnvironment;

	    static createFrom(source: any = {}) {
	        return new DiagnosisTaskResult(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.schema_version = source["schema_version"];
	        this.task_id = source["task_id"];
	        this.status = source["status"];
	        this.started_at = source["started_at"];
	        this.finished_at = source["finished_at"];
	        this.diagnosis = this.convertValues(source["diagnosis"], DiagResult);
	        this.traceroute = this.convertValues(source["traceroute"], TraceResult);
	        this.environment = this.convertValues(source["environment"], NetworkEnvironment);
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class IntegratedRemediation {
	    priority: number;
	    action: string;
	    title: string;
	    detail: string;
	    module: string;

	    static createFrom(source: any = {}) {
	        return new IntegratedRemediation(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.priority = source["priority"];
	        this.action = source["action"];
	        this.title = source["title"];
	        this.detail = source["detail"];
	        this.module = source["module"];
	    }
	}
	export class IntegratedFinding {
	    layer: string;
	    status: string;
	    title: string;
	    detail: string;

	    static createFrom(source: any = {}) {
	        return new IntegratedFinding(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.layer = source["layer"];
	        this.status = source["status"];
	        this.title = source["title"];
	        this.detail = source["detail"];
	    }
	}
	export class IntegratedDiagnosisResult {
	    schema_version: number;
	    task_id: string;
	    status: string;
	    overall_status: string;
	    message: string;
	    target_url: string;
	    domain: string;
	    started_at: string;
	    finished_at: string;
	    duration_ms: number;
	    completed: number;
	    total: number;
	    findings: IntegratedFinding[];
	    remediations: IntegratedRemediation[];
	    baseline: NetworkBaselineResult;
	    dns: DNSDiagnoseResult;
	    ports: PortTestResult;
	    http: DiagnosisTaskResult;
	    paths: PathCompareResult;
	    environment: NetworkEnvironment;

	    static createFrom(source: any = {}) {
	        return new IntegratedDiagnosisResult(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.schema_version = source["schema_version"];
	        this.task_id = source["task_id"];
	        this.status = source["status"];
	        this.overall_status = source["overall_status"];
	        this.message = source["message"];
	        this.target_url = source["target_url"];
	        this.domain = source["domain"];
	        this.started_at = source["started_at"];
	        this.finished_at = source["finished_at"];
	        this.duration_ms = source["duration_ms"];
	        this.completed = source["completed"];
	        this.total = source["total"];
	        this.findings = this.convertValues(source["findings"], IntegratedFinding);
	        this.remediations = this.convertValues(source["remediations"], IntegratedRemediation);
	        this.baseline = this.convertValues(source["baseline"], NetworkBaselineResult);
	        this.dns = this.convertValues(source["dns"], DNSDiagnoseResult);
	        this.ports = this.convertValues(source["ports"], PortTestResult);
	        this.http = this.convertValues(source["http"], DiagnosisTaskResult);
	        this.paths = this.convertValues(source["paths"], PathCompareResult);
	        this.environment = this.convertValues(source["environment"], NetworkEnvironment);
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class PortTestRemediation {
	    action: string;
	    title: string;
	    detail: string;

	    static createFrom(source: any = {}) {
	        return new PortTestRemediation(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.action = source["action"];
	        this.title = source["title"];
	        this.detail = source["detail"];
	    }
	}
	export class PortTestFinding {
	    code: string;
	    status: string;
	    title: string;
	    detail: string;

	    static createFrom(source: any = {}) {
	        return new PortTestFinding(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.status = source["status"];
	        this.title = source["title"];
	        this.detail = source["detail"];
	    }
	}
	export class PortProbeResult {
	    index: number;
	    port: number;
	    service: string;
	    status: string;
	    reachable: boolean;
	    latency_ms: number;
	    address?: string;
	    protocol: string;
	    http_status?: number;
	    tls_version?: string;
	    tls_alpn?: string;
	    tls_cert_subject?: string;
	    tls_cert_issuer?: string;
	    tls_cert_expires_at?: string;
	    tls_days_remaining: number;
	    tls_valid: boolean;
	    message: string;
	    error?: string;

	    static createFrom(source: any = {}) {
	        return new PortProbeResult(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.index = source["index"];
	        this.port = source["port"];
	        this.service = source["service"];
	        this.status = source["status"];
	        this.reachable = source["reachable"];
	        this.latency_ms = source["latency_ms"];
	        this.address = source["address"];
	        this.protocol = source["protocol"];
	        this.http_status = source["http_status"];
	        this.tls_version = source["tls_version"];
	        this.tls_alpn = source["tls_alpn"];
	        this.tls_cert_subject = source["tls_cert_subject"];
	        this.tls_cert_issuer = source["tls_cert_issuer"];
	        this.tls_cert_expires_at = source["tls_cert_expires_at"];
	        this.tls_days_remaining = source["tls_days_remaining"];
	        this.tls_valid = source["tls_valid"];
	        this.message = source["message"];
	        this.error = source["error"];
	    }
	}
	export class PortTestResult {
	    schema_version: number;
	    task_id: string;
	    status: string;
	    message: string;
	    target: string;
	    resolved_ips?: string[];
	    dns_error?: string;
	    started_at: string;
	    finished_at: string;
	    duration_ms: number;
	    total_count: number;
	    completed_count: number;
	    open_count: number;
	    refused_count: number;
	    timeout_count: number;
	    results: PortProbeResult[];
	    findings: PortTestFinding[];
	    remediations: PortTestRemediation[];
	    environment: NetworkEnvironment;

	    static createFrom(source: any = {}) {
	        return new PortTestResult(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.schema_version = source["schema_version"];
	        this.task_id = source["task_id"];
	        this.status = source["status"];
	        this.message = source["message"];
	        this.target = source["target"];
	        this.resolved_ips = source["resolved_ips"];
	        this.dns_error = source["dns_error"];
	        this.started_at = source["started_at"];
	        this.finished_at = source["finished_at"];
	        this.duration_ms = source["duration_ms"];
	        this.total_count = source["total_count"];
	        this.completed_count = source["completed_count"];
	        this.open_count = source["open_count"];
	        this.refused_count = source["refused_count"];
	        this.timeout_count = source["timeout_count"];
	        this.results = this.convertValues(source["results"], PortProbeResult);
	        this.findings = this.convertValues(source["findings"], PortTestFinding);
	        this.remediations = this.convertValues(source["remediations"], PortTestRemediation);
	        this.environment = this.convertValues(source["environment"], NetworkEnvironment);
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class PathCompareSample {
	    sequence: number;
	    success: boolean;
	    message: string;
	    failed_stage?: string;
	    error?: string;
	    tcp_connect_ms: number;
	    tls_handshake_ms: number;
	    ttfb_ms: number;
	    total_ms: number;
	    status_code: number;
	    proxy_status: string;
	    proxy_url?: string;
	    remote_ip?: string;
	    cf_colo?: string;

	    static createFrom(source: any = {}) {
	        return new PathCompareSample(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sequence = source["sequence"];
	        this.success = source["success"];
	        this.message = source["message"];
	        this.failed_stage = source["failed_stage"];
	        this.error = source["error"];
	        this.tcp_connect_ms = source["tcp_connect_ms"];
	        this.tls_handshake_ms = source["tls_handshake_ms"];
	        this.ttfb_ms = source["ttfb_ms"];
	        this.total_ms = source["total_ms"];
	        this.status_code = source["status_code"];
	        this.proxy_status = source["proxy_status"];
	        this.proxy_url = source["proxy_url"];
	        this.remote_ip = source["remote_ip"];
	        this.cf_colo = source["cf_colo"];
	    }
	}
	export class PathCompareRoute {
	    id: string;
	    label: string;
	    proxy_mode: string;
	    configured_url?: string;
	    effective_path: string;
	    same_as?: string;
	    samples: PathCompareSample[];
	    sample_count: number;
	    completed_count: number;
	    success_count: number;
	    success_rate: number;
	    p50_tcp_ms: number;
	    p50_tls_ms: number;
	    p50_ttfb_ms: number;
	    p50_total_ms: number;
	    jitter_ms: number;
	    remote_ip?: string;
	    proxy_url?: string;

	    static createFrom(source: any = {}) {
	        return new PathCompareRoute(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.label = source["label"];
	        this.proxy_mode = source["proxy_mode"];
	        this.configured_url = source["configured_url"];
	        this.effective_path = source["effective_path"];
	        this.same_as = source["same_as"];
	        this.samples = this.convertValues(source["samples"], PathCompareSample);
	        this.sample_count = source["sample_count"];
	        this.completed_count = source["completed_count"];
	        this.success_count = source["success_count"];
	        this.success_rate = source["success_rate"];
	        this.p50_tcp_ms = source["p50_tcp_ms"];
	        this.p50_tls_ms = source["p50_tls_ms"];
	        this.p50_ttfb_ms = source["p50_ttfb_ms"];
	        this.p50_total_ms = source["p50_total_ms"];
	        this.jitter_ms = source["jitter_ms"];
	        this.remote_ip = source["remote_ip"];
	        this.proxy_url = source["proxy_url"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class PathCompareFinding {
	    code: string;
	    level: string;
	    title: string;
	    detail: string;
	    recommended_path_id?: string;
	    improvement_percent: number;

	    static createFrom(source: any = {}) {
	        return new PathCompareFinding(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.level = source["level"];
	        this.title = source["title"];
	        this.detail = source["detail"];
	        this.recommended_path_id = source["recommended_path_id"];
	        this.improvement_percent = source["improvement_percent"];
	    }
	}
	export class PathCompareResult {
	    schema_version: number;
	    task_id: string;
	    status: string;
	    message: string;
	    target_url: string;
	    started_at: string;
	    finished_at: string;
	    duration_ms: number;
	    sample_count: number;
	    completed: number;
	    total: number;
	    finding: PathCompareFinding;
	    paths: PathCompareRoute[];
	    environment: NetworkEnvironment;

	    static createFrom(source: any = {}) {
	        return new PathCompareResult(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.schema_version = source["schema_version"];
	        this.task_id = source["task_id"];
	        this.status = source["status"];
	        this.message = source["message"];
	        this.target_url = source["target_url"];
	        this.started_at = source["started_at"];
	        this.finished_at = source["finished_at"];
	        this.duration_ms = source["duration_ms"];
	        this.sample_count = source["sample_count"];
	        this.completed = source["completed"];
	        this.total = source["total"];
	        this.finding = this.convertValues(source["finding"], PathCompareFinding);
	        this.paths = this.convertValues(source["paths"], PathCompareRoute);
	        this.environment = this.convertValues(source["environment"], NetworkEnvironment);
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class LocalNetworkAssessment {
	    status: string;
	    summary: string;
	    active_interfaces?: string[];
	    virtual_interfaces?: string[];
	    tunnel_interfaces?: string[];
	    mtu_risks?: string[];
	    default_route_count: number;
	    ipv4_default_routes: number;
	    ipv6_default_routes: number;
	    multiple_default_routes: boolean;
	    gateway_binding_complete: boolean;
	    route_takeover: boolean;
	    system_proxy_configured: boolean;
	    system_proxy_endpoint?: string;
	    dns_mode: string;
	    correlation: string;

	    static createFrom(source: any = {}) {
	        return new LocalNetworkAssessment(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	        this.summary = source["summary"];
	        this.active_interfaces = source["active_interfaces"];
	        this.virtual_interfaces = source["virtual_interfaces"];
	        this.tunnel_interfaces = source["tunnel_interfaces"];
	        this.mtu_risks = source["mtu_risks"];
	        this.default_route_count = source["default_route_count"];
	        this.ipv4_default_routes = source["ipv4_default_routes"];
	        this.ipv6_default_routes = source["ipv6_default_routes"];
	        this.multiple_default_routes = source["multiple_default_routes"];
	        this.gateway_binding_complete = source["gateway_binding_complete"];
	        this.route_takeover = source["route_takeover"];
	        this.system_proxy_configured = source["system_proxy_configured"];
	        this.system_proxy_endpoint = source["system_proxy_endpoint"];
	        this.dns_mode = source["dns_mode"];
	        this.correlation = source["correlation"];
	    }
	}
	export class IPStackProbeResult {
	    family: string;
	    available: boolean;
	    latency_ms: number;
	    local_ip?: string;
	    remote_ip?: string;
	    interface?: string;
	    message: string;

	    static createFrom(source: any = {}) {
	        return new IPStackProbeResult(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.family = source["family"];
	        this.available = source["available"];
	        this.latency_ms = source["latency_ms"];
	        this.local_ip = source["local_ip"];
	        this.remote_ip = source["remote_ip"];
	        this.interface = source["interface"];
	        this.message = source["message"];
	    }
	}
	export class NetworkBaselineResult {
	    schema_version: number;
	    task_id: string;
	    status: string;
	    message: string;
	    target_host: string;
	    started_at: string;
	    finished_at: string;
	    duration_ms: number;
	    routes?: DefaultRoute[];
	    ip_stacks: IPStackProbeResult[];
	    dns_probes: DNSProbeResult[];
	    dns_comparison: DNSComparison;
	    proxy_listeners?: ProxyListenerResult[];
	    findings: BaselineFinding[];
	    remediations?: BaselineRemediation[];
	    local_network: LocalNetworkAssessment;
	    environment: NetworkEnvironment;

	    static createFrom(source: any = {}) {
	        return new NetworkBaselineResult(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.schema_version = source["schema_version"];
	        this.task_id = source["task_id"];
	        this.status = source["status"];
	        this.message = source["message"];
	        this.target_host = source["target_host"];
	        this.started_at = source["started_at"];
	        this.finished_at = source["finished_at"];
	        this.duration_ms = source["duration_ms"];
	        this.routes = this.convertValues(source["routes"], DefaultRoute);
	        this.ip_stacks = this.convertValues(source["ip_stacks"], IPStackProbeResult);
	        this.dns_probes = this.convertValues(source["dns_probes"], DNSProbeResult);
	        this.dns_comparison = this.convertValues(source["dns_comparison"], DNSComparison);
	        this.proxy_listeners = this.convertValues(source["proxy_listeners"], ProxyListenerResult);
	        this.findings = this.convertValues(source["findings"], BaselineFinding);
	        this.remediations = this.convertValues(source["remediations"], BaselineRemediation);
	        this.local_network = this.convertValues(source["local_network"], LocalNetworkAssessment);
	        this.environment = this.convertValues(source["environment"], NetworkEnvironment);
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SiteTarget {
	    id: string;
	    name: string;
	    url: string;
	    category?: string;

	    static createFrom(source: any = {}) {
	        return new SiteTarget(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.url = source["url"];
	        this.category = source["category"];
	    }
	}
	export class SiteProbeResult {
	    index: number;
	    target: SiteTarget;
	    result: DiagResult;

	    static createFrom(source: any = {}) {
	        return new SiteProbeResult(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.index = source["index"];
	        this.target = this.convertValues(source["target"], SiteTarget);
	        this.result = this.convertValues(source["result"], DiagResult);
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SitePatrolResult {
	    schema_version: number;
	    task_id: string;
	    status: string;
	    message: string;
	    proxy_mode: string;
	    proxy_url?: string;
	    started_at: string;
	    finished_at: string;
	    duration_ms: number;
	    total_count: number;
	    success_count: number;
	    average_ms: number;
	    results: SiteProbeResult[];
	    findings: PatrolFinding[];
	    environment: NetworkEnvironment;

	    static createFrom(source: any = {}) {
	        return new SitePatrolResult(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.schema_version = source["schema_version"];
	        this.task_id = source["task_id"];
	        this.status = source["status"];
	        this.message = source["message"];
	        this.proxy_mode = source["proxy_mode"];
	        this.proxy_url = source["proxy_url"];
	        this.started_at = source["started_at"];
	        this.finished_at = source["finished_at"];
	        this.duration_ms = source["duration_ms"];
	        this.total_count = source["total_count"];
	        this.success_count = source["success_count"];
	        this.average_ms = source["average_ms"];
	        this.results = this.convertValues(source["results"], SiteProbeResult);
	        this.findings = this.convertValues(source["findings"], PatrolFinding);
	        this.environment = this.convertValues(source["environment"], NetworkEnvironment);
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class StabilitySample {
	    sequence: number;
	    checked_at: string;
	    latency_ms: number;
	    success: boolean;
	    status_code: number;
	    remote_ip?: string;
	    cf_ray?: string;
	    cf_colo?: string;
	    error?: string;

	    static createFrom(source: any = {}) {
	        return new StabilitySample(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sequence = source["sequence"];
	        this.checked_at = source["checked_at"];
	        this.latency_ms = source["latency_ms"];
	        this.success = source["success"];
	        this.status_code = source["status_code"];
	        this.remote_ip = source["remote_ip"];
	        this.cf_ray = source["cf_ray"];
	        this.cf_colo = source["cf_colo"];
	        this.error = source["error"];
	    }
	}
	export class StabilityRouteChange {
	    sequence: number;
	    checked_at: string;
	    previous: string;
	    current: string;

	    static createFrom(source: any = {}) {
	        return new StabilityRouteChange(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sequence = source["sequence"];
	        this.checked_at = source["checked_at"];
	        this.previous = source["previous"];
	        this.current = source["current"];
	    }
	}
	export class StabilityIncident {
	    type: string;
	    started_at: string;
	    ended_at: string;
	    start_sequence: number;
	    end_sequence: number;
	    sample_count: number;
	    peak_latency_ms: number;
	    resolved: boolean;
	    detail: string;

	    static createFrom(source: any = {}) {
	        return new StabilityIncident(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.type = source["type"];
	        this.started_at = source["started_at"];
	        this.ended_at = source["ended_at"];
	        this.start_sequence = source["start_sequence"];
	        this.end_sequence = source["end_sequence"];
	        this.sample_count = source["sample_count"];
	        this.peak_latency_ms = source["peak_latency_ms"];
	        this.resolved = source["resolved"];
	        this.detail = source["detail"];
	    }
	}
	export class StabilityResult {
	    schema_version: number;
	    task_id: string;
	    status: string;
	    message: string;
	    target_url: string;
	    proxy_mode: string;
	    proxy_url?: string;
	    started_at: string;
	    finished_at: string;
	    duration_ms: number;
	    configured_duration_ms: number;
	    interval_ms: number;
	    sample_count: number;
	    success_count: number;
	    failure_rate: number;
	    availability_percent: number;
	    average_ms: number;
	    minimum_ms: number;
	    maximum_ms: number;
	    p50_ms: number;
	    p95_ms: number;
	    jitter_ms: number;
	    latency_threshold_ms: number;
	    failure_threshold_percent: number;
	    slow_count: number;
	    longest_failure_streak: number;
	    incidents: StabilityIncident[];
	    route_changes: StabilityRouteChange[];
	    finding: PatrolFinding;
	    samples: StabilitySample[];
	    environment: NetworkEnvironment;

	    static createFrom(source: any = {}) {
	        return new StabilityResult(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.schema_version = source["schema_version"];
	        this.task_id = source["task_id"];
	        this.status = source["status"];
	        this.message = source["message"];
	        this.target_url = source["target_url"];
	        this.proxy_mode = source["proxy_mode"];
	        this.proxy_url = source["proxy_url"];
	        this.started_at = source["started_at"];
	        this.finished_at = source["finished_at"];
	        this.duration_ms = source["duration_ms"];
	        this.configured_duration_ms = source["configured_duration_ms"];
	        this.interval_ms = source["interval_ms"];
	        this.sample_count = source["sample_count"];
	        this.success_count = source["success_count"];
	        this.failure_rate = source["failure_rate"];
	        this.availability_percent = source["availability_percent"];
	        this.average_ms = source["average_ms"];
	        this.minimum_ms = source["minimum_ms"];
	        this.maximum_ms = source["maximum_ms"];
	        this.p50_ms = source["p50_ms"];
	        this.p95_ms = source["p95_ms"];
	        this.jitter_ms = source["jitter_ms"];
	        this.latency_threshold_ms = source["latency_threshold_ms"];
	        this.failure_threshold_percent = source["failure_threshold_percent"];
	        this.slow_count = source["slow_count"];
	        this.longest_failure_streak = source["longest_failure_streak"];
	        this.incidents = this.convertValues(source["incidents"], StabilityIncident);
	        this.route_changes = this.convertValues(source["route_changes"], StabilityRouteChange);
	        this.finding = this.convertValues(source["finding"], PatrolFinding);
	        this.samples = this.convertValues(source["samples"], StabilitySample);
	        this.environment = this.convertValues(source["environment"], NetworkEnvironment);
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class HistoryRecord {
	    schema_version: number;
	    kind: string;
	    history_id: string;
	    created_at: string;
	    result: DiagResult;
	    stability?: StabilityResult;
	    site_patrol?: SitePatrolResult;
	    cdn_optimize?: CDNOptimizeResult;
	    network_baseline?: NetworkBaselineResult;
	    path_compare?: PathCompareResult;
	    dns_diagnosis?: DNSDiagnoseResult;
	    port_test?: PortTestResult;
	    integrated?: IntegratedDiagnosisResult;
	    traceroute?: TraceResult;
	    environment: NetworkEnvironment;

	    static createFrom(source: any = {}) {
	        return new HistoryRecord(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.schema_version = source["schema_version"];
	        this.kind = source["kind"];
	        this.history_id = source["history_id"];
	        this.created_at = source["created_at"];
	        this.result = this.convertValues(source["result"], DiagResult);
	        this.stability = this.convertValues(source["stability"], StabilityResult);
	        this.site_patrol = this.convertValues(source["site_patrol"], SitePatrolResult);
	        this.cdn_optimize = this.convertValues(source["cdn_optimize"], CDNOptimizeResult);
	        this.network_baseline = this.convertValues(source["network_baseline"], NetworkBaselineResult);
	        this.path_compare = this.convertValues(source["path_compare"], PathCompareResult);
	        this.dns_diagnosis = this.convertValues(source["dns_diagnosis"], DNSDiagnoseResult);
	        this.port_test = this.convertValues(source["port_test"], PortTestResult);
	        this.integrated = this.convertValues(source["integrated"], IntegratedDiagnosisResult);
	        this.traceroute = this.convertValues(source["traceroute"], TraceResult);
	        this.environment = this.convertValues(source["environment"], NetworkEnvironment);
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

	export class ImportedReportSummary {
	    format_version: number;
	    application: string;
	    app_version: string;
	    generated_at: string;
	    history_id: string;
	    kind: string;
	    kind_label: string;
	    checked_at: string;
	    target: string;
	    success: boolean;
	    conclusion: string;
	    metrics?: Record<string, any>;
	    proxy?: Record<string, any>;
	    traceroute?: Record<string, any>;

	    static createFrom(source: any = {}) {
	        return new ImportedReportSummary(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.format_version = source["format_version"];
	        this.application = source["application"];
	        this.app_version = source["app_version"];
	        this.generated_at = source["generated_at"];
	        this.history_id = source["history_id"];
	        this.kind = source["kind"];
	        this.kind_label = source["kind_label"];
	        this.checked_at = source["checked_at"];
	        this.target = source["target"];
	        this.success = source["success"];
	        this.conclusion = source["conclusion"];
	        this.metrics = source["metrics"];
	        this.proxy = source["proxy"];
	        this.traceroute = source["traceroute"];
	    }
	}
	export class ImportedReport {
	    filename: string;
	    summary: ImportedReportSummary;
	    record: HistoryRecord;
	    files: string[];
	    warnings?: string[];

	    static createFrom(source: any = {}) {
	        return new ImportedReport(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.filename = source["filename"];
	        this.summary = this.convertValues(source["summary"], ImportedReportSummary);
	        this.record = this.convertValues(source["record"], HistoryRecord);
	        this.files = source["files"];
	        this.warnings = source["warnings"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

	export class IntegratedDiagnosisOptions {
	    task_id: string;
	    target_url: string;
	    proxy_mode: string;
	    proxy_url?: string;

	    static createFrom(source: any = {}) {
	        return new IntegratedDiagnosisOptions(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.task_id = source["task_id"];
	        this.target_url = source["target_url"];
	        this.proxy_mode = source["proxy_mode"];
	        this.proxy_url = source["proxy_url"];
	    }
	}




	export class NetworkBaselineOptions {
	    task_id: string;
	    target_host: string;

	    static createFrom(source: any = {}) {
	        return new NetworkBaselineOptions(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.task_id = source["task_id"];
	        this.target_host = source["target_host"];
	    }
	}




	export class PathCompareOptions {
	    task_id: string;
	    target_url: string;
	    manual_proxy_url?: string;
	    samples: number;

	    static createFrom(source: any = {}) {
	        return new PathCompareOptions(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.task_id = source["task_id"];
	        this.target_url = source["target_url"];
	        this.manual_proxy_url = source["manual_proxy_url"];
	        this.samples = source["samples"];
	    }
	}






	export class PortTestOptions {
	    task_id: string;
	    target: string;
	    ports: number[];
	    concurrency: number;

	    static createFrom(source: any = {}) {
	        return new PortTestOptions(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.task_id = source["task_id"];
	        this.target = source["target"];
	        this.ports = source["ports"];
	        this.concurrency = source["concurrency"];
	    }
	}




	export class SitePatrolOptions {
	    task_id: string;
	    targets: SiteTarget[];
	    proxy_mode: string;
	    proxy_url?: string;
	    concurrency: number;

	    static createFrom(source: any = {}) {
	        return new SitePatrolOptions(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.task_id = source["task_id"];
	        this.targets = this.convertValues(source["targets"], SiteTarget);
	        this.proxy_mode = source["proxy_mode"];
	        this.proxy_url = source["proxy_url"];
	        this.concurrency = source["concurrency"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}




	export class StabilityOptions {
	    task_id: string;
	    target_url: string;
	    proxy_mode: string;
	    proxy_url?: string;
	    duration_seconds: number;
	    interval_ms: number;
	    latency_threshold_ms: number;
	    failure_threshold_percent: number;

	    static createFrom(source: any = {}) {
	        return new StabilityOptions(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.task_id = source["task_id"];
	        this.target_url = source["target_url"];
	        this.proxy_mode = source["proxy_mode"];
	        this.proxy_url = source["proxy_url"];
	        this.duration_seconds = source["duration_seconds"];
	        this.interval_ms = source["interval_ms"];
	        this.latency_threshold_ms = source["latency_threshold_ms"];
	        this.failure_threshold_percent = source["failure_threshold_percent"];
	    }
	}






	export class UpdateRelease {
	    version: string;
	    notes?: string;
	    published_at?: string;
	    asset_name: string;
	    asset_size: number;

	    static createFrom(source: any = {}) {
	        return new UpdateRelease(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.notes = source["notes"];
	        this.published_at = source["published_at"];
	        this.asset_name = source["asset_name"];
	        this.asset_size = source["asset_size"];
	    }
	}
	export class UpdateState {
	    phase: string;
	    current_version: string;
	    enabled: boolean;
	    release?: UpdateRelease;
	    progress: number;
	    downloaded: number;
	    total: number;
	    last_checked_at?: string;
	    error?: string;

	    static createFrom(source: any = {}) {
	        return new UpdateState(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.phase = source["phase"];
	        this.current_version = source["current_version"];
	        this.enabled = source["enabled"];
	        this.release = this.convertValues(source["release"], UpdateRelease);
	        this.progress = source["progress"];
	        this.downloaded = source["downloaded"];
	        this.total = source["total"];
	        this.last_checked_at = source["last_checked_at"];
	        this.error = source["error"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

