const $ = (selector, root = document) => root.querySelector(selector);
const $$ = (selector, root = document) => [...root.querySelectorAll(selector)];

const sceneMeta = {
  network: {
    kicker: '场景 01',
    title: '整体网速检查',
    description: '检查本机网络环境与常用站点的真实访问质量，识别 DNS、出口、代理和跨站共性问题。',
    tools: ['network-full', 'baseline', 'patrol']
  },
  site: {
    kicker: '场景 02',
    title: '指定网站检查',
    description: '输入一个网站，自动检查解析、端口、服务、链路和连接路径；需要时再继续深挖或持续观察。',
    tools: ['site-full']
  },
  cloudflare: {
    kicker: '场景 03',
    title: 'Cloudflare 网站加速',
    description: '验证真实域名和候选节点，比较当前 DNS 与推荐 IP，并提供明确、可回滚的应用方式。',
    tools: ['cf-optimize', 'cf-status']
  }
};

const toolMeta = {
  'network-full': {
    label: '整体检查', kicker: '主任务', title: '检查当前网络与常用站点',
    description: '并行采集网络基线和多站点访问质量，再归纳共性故障。', estimate: '约 1-3 分钟 · 每站最多读取 4 KiB',
    stages: [['本机环境', '接口、MTU 与路由'], ['DNS 对照', '系统与公共解析'], ['公网出口', 'IPv4 / IPv6'], ['站点巡检', '真实访问质量'], ['形成结论', '治理优先级']],
    boundary: '不会测量运营商签约带宽；结果来自本机环境和真实网站的小流量请求。'
  },
  baseline: {
    label: '网络基线', kicker: '专项工具', title: '采集本地网络基线',
    description: '检查接口、默认路由、MTU、DNS、公网出口和本地代理监听。', estimate: '约 20 秒 · 本地证据为主',
    stages: [['本机接口', '活动网卡与地址'], ['默认路由', '网关与 MTU'], ['DNS 对照', '四组解析器'], ['公网出口', 'IPv4 / IPv6']],
    boundary: '公共 DNS 结果不同可能来自 CDN 调度，不直接判定为污染。'
  },
  patrol: {
    label: '多站点巡检', kicker: '专项工具', title: '巡检常用与自定义站点',
    description: '并发检查一组网站的 DNS、TCP、TLS、TTFB、总耗时和实际连接路径。', estimate: '约 30-60 秒 · 最多 6 路并发',
    stages: [['加载目标', '确认站点清单'], ['并行请求', '每站最多 4 KiB'], ['分段计时', 'DNS 到总耗时'], ['共性分析', '跨站故障归因']],
    boundary: '只访问明确选择的站点；自定义 URL 不会用于网段扫描。'
  },
  'site-full': {
    label: '网站检查', kicker: '主任务', title: '检查一个网站',
    description: '自动完成 DNS、端口、HTTP/TLS、网络链路和连接策略对照。', estimate: '约 25 秒 · 默认检查 Web 服务与 15 跳链路',
    stages: [['域名解析', '系统与公共 DNS'], ['端口服务', 'TCP、TLS 与 HTTP'], ['网络链路', '路由与无响应跳点'], ['路径对照', '系统代理与直连'], ['形成结论', '异常与下一步']],
    boundary: '路由追踪反映本机到目标 IP 的路径，不展示 HTTP/SOCKS 代理内部路由。'
  },
  dns: {
    label: 'DNS 分析', kicker: '专项工具', title: '运行 DNS 深度诊断',
    description: '比较系统 DNS、Cloudflare/AliDNS UDP 与 Cloudflare/Google DoH。', estimate: '约 10 秒 · 查询 A / AAAA / CNAME / NS',
    stages: [['系统 DNS', '本机解析链路'], ['公共 UDP', 'Cloudflare / AliDNS'], ['DoH', 'Cloudflare / Google'], ['证据归因', '记录、TTL 与 AD']],
    boundary: '会把填写的域名提交给界面列出的公共 DNS 与 DoH 解析器。'
  },
  ports: {
    label: '端口协议', kicker: '专项工具', title: '检测端口与应用协议',
    description: '并发探测明确选择的 TCP 端口，并补充 HTTP、TLS、证书与 ALPN 证据。', estimate: '约 10-30 秒 · 单次最多 24 个端口',
    stages: [['解析主机', '获取目标地址'], ['TCP 探测', '开放与故障分类'], ['协议验证', 'HTTP / TLS / ALPN'], ['治理建议', '按预期服务判断']],
    boundary: '只检测当前主机和明确填写的端口，不扫描网段。关闭端口不一定是故障。'
  },
  monitor: {
    label: '持续监控', kicker: '专项工具', title: '持续观察网站稳定性',
    description: '按设定间隔执行小流量采样，记录可用率、分位延迟、抖动、事件和路径变化。', estimate: '10 秒至 15 分钟 · 主动停止仍保存证据',
    stages: [['建立基线', '初始路径与延迟'], ['持续采样', '小流量 HTTP'], ['阈值检测', '失败与高延迟'], ['事件归档', '路径和节点变化']],
    boundary: '这是目标网站稳定性监控，不等同于宽带下载速度测试。'
  },
  path: {
    label: '路径对照', kicker: '专项工具', title: '比较不同连接策略',
    description: '对同一目标比较系统代理、强制直连和可选手动代理。', estimate: '每路径 3 或 5 次 · 每次最多读取 4 KiB',
    stages: [['系统代理', '采集分段耗时'], ['强制直连', '绕过显式代理'], ['手动代理', '可选路径'], ['对照分析', '成功率与 P50']],
    boundary: '只提供连接策略建议，不会静默修改系统代理。TUN 仍可能接管直连流量。'
  },
  'cf-optimize': {
    label: 'IP 优选', kicker: '主任务', title: 'Cloudflare IP 优选与验证',
    description: '候选发现、延迟丢包筛选、可选下载测速、真实域名验证与治理输出。', estimate: '约 2-10 分钟 · 取决于候选量与下载测速',
    stages: [['验证域名', '确认 Cloudflare 代理'], ['生成候选', '官方或自定义网段'], ['延迟筛选', 'TCPing / HTTPing'], ['下载测速', '可选入围节点'], ['域名验证', 'Host / SNI / CF-Ray']],
    boundary: '候选结果必须用真实域名验证；下载测试会产生实际流量，结果不等同于宽带测速。'
  },
  'cf-status': {
    label: '加速与恢复', kicker: '专项工具', title: '查看加速状态与恢复点',
    description: '确认内置兼容网关、当前推荐 IP、系统代理恢复点和最近应用记录。', estimate: '实时读取本机状态',
    stages: [['读取网关', '监听与健康状态'], ['检查路由', '当前流量出口'], ['验证恢复点', '原系统代理快照']],
    boundary: '停止时只在当前系统设置仍与应用后的状态一致时恢复，避免覆盖用户后续修改。'
  }
};

const historyRecords = [
  { id: 1, scene: 'site', tool: 'site-full', type: '指定网站检查', target: 'www.cloudflare.com', time: '今天 14:32', metric: '总耗时 438 ms', finding: 'DNS 响应偏慢' },
  { id: 2, scene: 'site', tool: 'site-full', type: '指定网站检查', target: 'www.cloudflare.com', time: '昨天 16:08', metric: '总耗时 612 ms', finding: '系统代理耗时偏高' },
  { id: 3, scene: 'network', tool: 'network-full', type: '整体网速检查', target: '当前设备网络', time: '11 月 4 日 09:20', metric: '11 / 12 正常', finding: '国际站点延迟偏高' },
  { id: 4, scene: 'cloudflare', tool: 'cf-optimize', type: 'Cloudflare IP 优选', target: 'example.com', time: '11 月 2 日 18:41', metric: '推荐 104.18.7.12', finding: '首包预计降低 37%' }
];

const state = { scene: null, tool: null, timer: null, inlineMonitorTimer: null, running: false, completedStages: 0, toastTimer: null, modalAction: null, cfActive: true };

function urlControl(id, value = 'www.cloudflare.com', required = true) {
  return `<div class="control"><select aria-label="请求协议"><option>HTTPS</option><option>HTTP</option></select><input id="${id}" value="${value}" autocomplete="off" ${required ? 'required' : ''}></div>`;
}

function proxyControl(prefix) {
  return `<div class="field full"><span>连接策略</span><div class="segmented" data-proxy-group="${prefix}"><label><input type="radio" name="${prefix}-proxy" value="system" checked><span>自动代理</span></label><label><input type="radio" name="${prefix}-proxy" value="direct"><span>强制直连</span></label><label><input type="radio" name="${prefix}-proxy" value="manual"><span>手动代理</span></label></div></div><div class="field full" id="${prefix}-manual" hidden><label>代理端点 <small>HTTP · HTTPS · SOCKS5</small></label><div class="control"><span class="prefix">PX</span><input value="127.0.0.1:7890" autocomplete="off"></div></div>`;
}

function targetList() {
  return `<div class="target-list" id="patrol-target-list">
    ${[['Cloudflare', 'https://www.cloudflare.com'], ['百度', 'https://www.baidu.com'], ['GitHub', 'https://github.com'], ['Google', 'https://www.google.com'], ['YouTube', 'https://www.youtube.com'], ['OpenAI', 'https://openai.com']].map(([name, url], index) => `<label><input type="checkbox" ${index < 4 ? 'checked' : ''}><span><strong>${name}</strong><small>${url}</small></span></label>`).join('')}
  </div>`;
}

function presetSummary(title, copy, meta) {
  return `<div class="preset-summary"><span class="preset-icon">AUTO</span><div><strong>${title}</strong><p>${copy}</p></div><small>${meta}</small></div>`;
}

function advancedSettings(title, summary, content) {
  return `<details class="advanced"><summary><span>${title}</span><small>${summary}</small></summary><div class="advanced-body">${content}</div></details>`;
}

function advancedSection(title, copy, content) {
  return `<section class="advanced-section"><header><strong>${title}</strong><span>${copy}</span></header>${content}</section>`;
}

function renderForm(tool) {
  if (tool === 'network-full') return `${presetSummary('使用推荐检查方案', '自动检查网络基线、DNS、代理环境和 4 个常用站点。', '约 1-3 分钟')}${advancedSettings('高级设置', '默认方案可直接开始', `<div class="form-grid">
    <div class="field"><label>参考域名 <small>用于 DNS 与公网出口</small></label><div class="control"><span class="prefix">DNS</span><input value="www.cloudflare.com" required></div></div>
    <div class="field"><label>巡检并发数</label><div class="control"><select><option>2</option><option selected>4</option><option>6</option></select></div></div>
    <div class="field full"><span>检查范围</span><div class="check-grid"><label><input type="checkbox" checked>网络基线</label><label><input type="checkbox" checked>DNS 对照</label><label><input type="checkbox" checked>常用站点</label><label><input type="checkbox" checked>代理环境</label><label><input type="checkbox">IPv6 深度检查</label><label><input type="checkbox">保存为对照基线</label></div></div>
    <div class="field full"><span>巡检目标 <small>已选择 4 / 6</small></span>${targetList()}</div>
    ${proxyControl('network')}
  </div>`)}`;
  if (tool === 'baseline') return `${presetSummary('自动采集本机网络', '检查活动接口、默认路由、MTU、DNS、IPv4 / IPv6 出口和本地代理。', '无需配置')}${advancedSettings('高级设置', '1 个可选参数', `<div class="form-grid"><div class="field full"><label>参考域名 <small>用于 DNS 与公网出口对照</small></label><div class="control"><span class="prefix">DNS</span><input value="www.cloudflare.com" required></div><p class="field-message">只有需要对照特定域名时才需修改。</p></div></div>`)}`;
  if (tool === 'patrol') return `${presetSummary('巡检 4 个常用站点', 'Cloudflare、百度、GitHub、Google；自动沿用当前连接策略。', '每站最多 4 KiB')}${advancedSettings('高级设置', '目标、代理与并发', `<div class="form-grid"><div class="field"><label>连接策略</label><div class="control"><select id="patrol-proxy"><option value="system">自动代理</option><option value="direct">绕过代理</option><option value="manual">手动代理</option></select></div></div><div class="field"><label>并发数</label><div class="control"><select><option>2</option><option selected>4</option><option>6</option></select></div></div><div class="field full" id="patrol-manual" hidden><label>代理地址</label><div class="control"><span class="prefix">PX</span><input value="127.0.0.1:7890"></div></div><div class="field full"><span>巡检目标 <small>已选择 4 / 6</small></span>${targetList()}</div><div class="field full"><span>添加自定义目标</span><div class="inline-add"><div class="control"><input id="custom-name" placeholder="站点名称"></div><div class="control"><input id="custom-url" placeholder="https://example.com/"></div><button class="button secondary" id="add-target" type="button">添加目标</button></div></div></div>`)}`;
  if (tool === 'site-full') return `<div class="form-grid"><div class="field full"><label>目标域名或 URL</label>${urlControl('site-target')}</div></div>${presetSummary('自动运行完整网站检查', '包含 DNS、端口、HTTP/TLS、网络链路和连接策略对照。', '约 25 秒')}${advancedSettings('高级设置', '默认参数可直接开始', `${advancedSection('连接策略', '自动识别当前代理环境', `<div class="form-grid">${proxyControl('site')}<div class="field"><label>每条路径采样</label><div class="control"><select><option selected>3 次</option><option>5 次</option></select></div></div><div class="field"><label>路由最大跳数</label><div class="control"><select><option>10 跳</option><option selected>15 跳</option><option>30 跳</option></select></div></div></div>`)}`+
    `${advancedSection('服务与端口', '默认检查 Web 服务 80 / 443', `<div class="form-grid"><div class="field full"><label>端口列表 <small>最多 24 个</small></label><div class="control"><input id="site-port-list" value="80, 443" required></div></div><div class="field full"><span>常用端口</span><div class="check-grid" id="site-common-ports">${[['22','SSH'],['53','DNS'],['80','HTTP'],['443','HTTPS'],['3306','MySQL'],['5432','PostgreSQL'],['6379','Redis'],['8080','HTTP'],['8443','HTTPS']].map(([port, name]) => `<label><input type="checkbox" value="${port}" ${port === '80' || port === '443' ? 'checked' : ''}>${port} ${name}</label>`).join('')}</div></div></div>`)}`)}`;
  if (tool === 'dns') return `<div class="form-grid"><div class="field full"><label>诊断域名</label><div class="control"><span class="prefix">DNS</span><input value="www.cloudflare.com" required></div><p class="field-message">查询系统 DNS、Cloudflare/AliDNS UDP 与 Cloudflare/Google DoH。</p></div></div>`;
  if (tool === 'ports') return `<div class="form-grid"><div class="field"><label>目标主机或 URL</label><div class="control"><input value="www.cloudflare.com" required></div></div><div class="field"><label>检测方案</label><div class="control"><select id="port-preset"><option value="80, 443" selected>Web 服务 · 80 / 443</option><option value="22">SSH · 22</option><option value="53">DNS · 53</option><option value="custom">自定义端口</option></select></div></div></div>${advancedSettings('高级设置', '端口明细与自定义', `<div class="form-grid"><div class="field full"><label>端口列表 <small>最多 24 个</small></label><div class="control"><input id="port-list" value="80, 443" required></div></div><div class="field full"><span>常用端口</span><div class="check-grid" id="common-ports">${[['22','SSH'],['53','DNS'],['80','HTTP'],['443','HTTPS'],['3306','MySQL'],['5432','PostgreSQL'],['6379','Redis'],['8080','HTTP'],['8443','HTTPS']].map(([port, name]) => `<label><input type="checkbox" value="${port}" ${port === '80' || port === '443' ? 'checked' : ''}>${port} ${name}</label>`).join('')}</div></div></div>`)}`;
  if (tool === 'monitor') return `<div class="form-grid"><div class="field full"><label>目标域名或 URL</label>${urlControl('monitor-target')}</div><div class="field"><label>测试时长</label><div class="control"><select><option>10 秒 · 快速验证</option><option selected>30 秒</option><option>2 分钟</option><option>5 分钟</option><option>15 分钟</option></select></div></div></div>${advancedSettings('高级设置', '采样与告警阈值', `<div class="form-grid three"><div class="field"><label>采样间隔</label><div class="control"><select><option selected>1 秒</option><option>2 秒</option><option>5 秒</option><option>10 秒</option></select></div></div><div class="field"><label>延迟阈值</label><div class="control"><select><option>300 ms</option><option selected>500 ms</option><option>1000 ms</option><option>2000 ms</option></select></div></div><div class="field"><label>失败率阈值</label><div class="control"><select><option>1%</option><option>5%</option><option selected>10%</option><option>20%</option></select></div></div></div>`)}`;
  if (tool === 'path') return `<div class="form-grid"><div class="field full"><label>目标域名或 URL</label>${urlControl('path-target')}</div></div>${advancedSettings('高级设置', '手动代理与采样次数', `<div class="form-grid"><div class="field"><label>手动代理 <small>可选路径</small></label><div class="control"><span class="prefix">PX</span><input value="127.0.0.1:7890"></div></div><div class="field"><label>每路径采样</label><div class="control"><select><option selected>3 次</option><option>5 次</option></select></div></div></div>`)}`;
  if (tool === 'cf-optimize') return `<div class="form-grid"><div class="field full"><label>目标域名</label>${urlControl('cf-domain', 'example.com')}</div></div><div class="compatibility-strip"><i></i><div><strong id="compat-title">检测到 Clash / Mihomo 与 TUN</strong><span id="compat-detail">将自动使用与当前网络一致的测试出口</span></div><button class="icon-quiet" id="refresh-compat" type="button" title="重新识别代理环境">↻</button></div>${advancedSettings('高级设置', '已使用推荐参数', `${advancedSection('连接与验证', '目标如何请求及验证', `<div class="form-grid three"><div class="field"><label>测试路径</label><div class="control"><span class="prefix">URI</span><input value="/"></div></div><div class="field"><label>延迟模式</label><div class="control"><select id="cf-mode"><option selected>TCPing + 域名验证</option><option>HTTPing</option></select></div></div><div class="field"><label>测试端口</label><div class="control"><input type="number" value="443"></div></div><div class="field"><label>测试出口</label><div class="control"><select id="cf-route"><option value="auto" selected>自动兼容</option><option value="direct">本机网络</option><option value="proxy">现有代理</option></select></div></div><div class="field" id="cf-upstream" hidden><label>上游代理</label><div class="control"><input value="http://127.0.0.1:7890"></div></div><div class="field"><label>验证范围</label><div class="control"><select><option selected>智能验证</option><option>网络层优选</option><option>全程域名验证</option></select></div></div></div>`)}`+
    `${advancedSection('候选与筛选', '候选来源、采样及入围条件', `<div class="toggle-row"><label><input type="checkbox" checked>Cloudflare IPv4</label><label><input type="checkbox">Cloudflare IPv6</label><label><input type="checkbox">IPv4 全量测试</label><label><input type="checkbox">保留调试错误</label></div><div class="field"><div class="file-row"><span>自定义 IP / CIDR <small>支持 IPv4、IPv6 和网段</small></span><button class="text-button" id="import-candidates" type="button">导入 TXT / CSV</button></div><div class="control"><textarea id="candidate-input" placeholder="每行一个 IP 或 CIDR，例如 104.16.0.0/13"></textarea></div></div><div class="form-grid three"><div class="field"><label>单 IP 采样</label><div class="control"><select><option selected>4 次</option><option>5 次</option><option>10 次</option></select></div></div><div class="field"><label>并发数</label><div class="control"><select><option>50</option><option>100</option><option selected>200</option><option>500</option><option>1000</option></select></div></div><div class="field"><label>IPv6 每网段抽样</label><div class="control"><input type="number" value="16"></div></div><div class="field"><label>最低延迟</label><div class="control"><input type="number" value="0"><span class="prefix">ms</span></div></div><div class="field"><label>最高延迟</label><div class="control"><input type="number" value="9999"><span class="prefix">ms</span></div></div><div class="field"><label>最高丢包率</label><div class="control"><select><option selected>100%</option><option>50%</option><option>20%</option><option>0%</option></select></div></div><div class="field"><label>HTTP 状态码</label><div class="control"><input value="200, 301, 302"></div></div><div class="field"><label>地区筛选</label><div class="control"><input placeholder="HKG, NRT, LAX"></div></div><div class="field"><label>展示数量</label><div class="control"><input type="number" value="10"></div></div></div>`)}`+
    `${advancedSection('下载测速', '只对入围节点产生实际流量', `<div class="toggle-row"><label><input id="download-enabled" type="checkbox" checked>启用下载测速</label></div><div class="form-grid three" id="download-options"><div class="field full"><label>下载测速地址</label><div class="control"><input value="https://speed.cloudflare.com/__down?bytes=200000000"></div></div><div class="field"><label>测速数量</label><div class="control"><input type="number" value="10"></div></div><div class="field"><label>单节点时长</label><div class="control"><input type="number" value="10"><span class="prefix">s</span></div></div><div class="field"><label>速度下限</label><div class="control"><input type="number" value="0"><span class="prefix">MB/s</span></div></div></div>`)}`)}`;
  return `<div class="acceleration-banner"><i></i><div><strong>${state.cfActive ? 'Cloudflare IP 优选正在生效' : '未启用 CF IP 加速'}</strong><p>${state.cfActive ? 'example.com → 104.18.7.12 · 内置网关 127.0.0.1:17891' : '应用后仅接管系统 HTTP / HTTPS 代理，并保存原设置恢复点。'}</p></div><button class="button ${state.cfActive ? 'danger' : 'primary'} compact" id="status-action" type="button">${state.cfActive ? '停止并恢复' : '前往优选'}</button></div><div class="form-grid three" style="margin-top:13px"><div class="field"><label>网关状态</label><div class="control"><input value="${state.cfActive ? '运行正常' : '未启动'}" readonly></div></div><div class="field"><label>系统代理恢复点</label><div class="control"><input value="${state.cfActive ? '已保存 · 14:08' : '无'}" readonly></div></div><div class="field"><label>实际出口</label><div class="control"><input value="Clash / Mihomo + TUN" readonly></div></div></div>`;
}

function openHome() {
  stopTask(false);
  state.scene = null; state.tool = null;
  $('#scene-hub').hidden = false; $('#scene-workspace').hidden = true;
  $$('.scene-nav button').forEach(button => button.classList.remove('active'));
  $('#page-title').textContent = '选择任务场景';
  $('#page-subtitle').textContent = '从要完成的事情开始，工具能力按需出现';
}

function openScene(scene, preferredTool) {
  stopTask(false);
  state.scene = scene;
  $('#scene-hub').hidden = true; $('#scene-workspace').hidden = false;
  const meta = sceneMeta[scene];
  $('#scene-kicker').textContent = meta.kicker; $('#scene-title').textContent = meta.title; $('#scene-description').textContent = meta.description;
  $('#page-title').textContent = meta.title; $('#page-subtitle').textContent = meta.description;
  $$('.scene-nav button').forEach(button => button.classList.toggle('active', button.dataset.openScene === scene));
  $('#tool-tabs').hidden = meta.tools.length === 1;
  $('#tool-tabs').innerHTML = meta.tools.length > 1 ? meta.tools.map(tool => `<button type="button" data-tool="${tool}">${toolMeta[tool].label}</button>`).join('') : '';
  $$('[data-tool]', $('#tool-tabs')).forEach(button => button.addEventListener('click', () => selectTool(button.dataset.tool)));
  selectTool(preferredTool && meta.tools.includes(preferredTool) ? preferredTool : meta.tools[0]);
  $('.workspace').scrollTo({ top: 0, behavior: 'auto' });
}

function selectTool(tool) {
  stopTask(false);
  state.tool = tool;
  const meta = toolMeta[tool];
  $$('[data-tool]', $('#tool-tabs')).forEach(button => button.classList.toggle('active', button.dataset.tool === tool));
  $('#tool-kicker').textContent = meta.kicker; $('#tool-title').textContent = meta.title; $('#tool-description').textContent = meta.description;
  $('#task-estimate').textContent = meta.estimate; $('#summary-title').textContent = meta.title; $('#summary-boundary').textContent = meta.boundary;
  $('#summary-steps').innerHTML = meta.stages.map((step, index) => `<div><b>${index + 1}</b><span><strong>${step[0]}</strong><span>${step[1]}</span></span></div>`).join('');
  $('#tool-form').innerHTML = renderForm(tool); $('#tool-info').title = meta.boundary;
  $('#run-button').textContent = tool === 'cf-status' ? '刷新状态' : tool === 'monitor' ? '开始监控' : tool === 'cf-optimize' ? '开始优选' : '开始检查';
  $('#execution-panel').hidden = true; $('#result-panel').hidden = true;
  bindFormInteractions();
}

function bindFormInteractions() {
  $$('[data-proxy-group]').forEach(group => group.addEventListener('change', event => {
    const target = $(`#${group.dataset.proxyGroup}-manual`);
    if (target) target.hidden = event.target.value !== 'manual';
  }));
  $('#patrol-proxy')?.addEventListener('change', event => { $('#patrol-manual').hidden = event.target.value !== 'manual'; });
  $('#add-target')?.addEventListener('click', () => {
    const name = $('#custom-name').value.trim(); const url = $('#custom-url').value.trim();
    if (!name || !url) return showToast('请填写站点名称和完整 URL');
    $('#patrol-target-list').insertAdjacentHTML('beforeend', `<label><input type="checkbox" checked><span><strong>${name}</strong><small>${url}</small></span></label>`);
    $('#custom-name').value = ''; $('#custom-url').value = ''; showToast('已添加自定义巡检目标');
  });
  $('#common-ports')?.addEventListener('change', () => {
    $('#port-list').value = $$('input:checked', $('#common-ports')).map(input => input.value).join(', ');
  });
  $('#site-common-ports')?.addEventListener('change', () => {
    $('#site-port-list').value = $$('input:checked', $('#site-common-ports')).map(input => input.value).join(', ');
  });
  $('#port-preset')?.addEventListener('change', event => {
    if (event.target.value === 'custom') {
      $('.advanced')?.setAttribute('open', '');
      $('#port-list')?.focus();
      return;
    }
    $('#port-list').value = event.target.value;
    $$('input', $('#common-ports')).forEach(input => { input.checked = event.target.value.split(', ').includes(input.value); });
  });
  $('#refresh-compat')?.addEventListener('click', () => {
    $('#compat-title').textContent = '正在重新识别代理环境'; $('#compat-detail').textContent = '检查系统代理、本地监听端口与活动 TUN';
    setTimeout(() => { $('#compat-title').textContent = '检测到 Clash / Mihomo 与 TUN'; $('#compat-detail').textContent = '将自动使用与当前网络一致的测试出口'; showToast('代理环境已更新'); }, 650);
  });
  $('#cf-route')?.addEventListener('change', event => { $('#cf-upstream').hidden = event.target.value !== 'proxy'; });
  $('#download-enabled')?.addEventListener('change', event => { $('#download-options').hidden = !event.target.checked; });
  $('#import-candidates')?.addEventListener('click', () => { $('#candidate-input').value = '104.16.0.0/24\n104.17.0.0/24\n2606:4700::/48'; showToast('已导入 3 组候选 IP / CIDR'); });
  $('#status-action')?.addEventListener('click', () => {
    if (state.cfActive) confirmAction('停止并恢复原系统代理？', '系统会先验证当前设置与恢复点一致，再停止兼容网关并恢复原代理。', '停止并恢复', () => { state.cfActive = false; selectTool('cf-status'); $('#recovery-banner').hidden = true; showToast('已停止加速并恢复原系统代理'); });
    else openScene('cloudflare', 'cf-optimize');
  });
}

function startTask(event) {
  event.preventDefault();
  if (state.running) return stopTask(true);
  const invalid = $$('[required]', $('#tool-form')).find(input => !input.value.trim());
  if (invalid) { invalid.focus(); return showToast('请先填写必填目标'); }
  state.running = true; state.completedStages = 0;
  const meta = toolMeta[state.tool];
  $('#execution-panel').hidden = false; $('#result-panel').hidden = true;
  $('#execution-title').textContent = state.tool === 'monitor' ? '正在持续采样' : state.tool === 'cf-optimize' ? '正在筛选并验证候选节点' : '正在执行检查';
  $('#execution-detail').textContent = meta.title;
  $('#execution-stages').innerHTML = meta.stages.map((stage, index) => `<article class="${index === 0 ? 'running' : ''}"><b>${index + 1}</b><strong>${stage[0]}</strong><span>${index === 0 ? '进行中' : '等待'}</span></article>`).join('');
  $('#progress-bar').style.width = '0%'; $('#run-button').textContent = '停止任务'; $('#run-button').classList.add('danger'); $('#run-button').classList.remove('primary'); $('#reset-button').disabled = true;
  const rows = $$('.execution-stages article');
  state.timer = setInterval(() => {
    const row = rows[state.completedStages];
    row.className = 'done'; $('span', row).textContent = '完成'; state.completedStages += 1;
    $('#progress-bar').style.width = `${(state.completedStages / rows.length) * 100}%`;
    if (state.completedStages < rows.length) { rows[state.completedStages].className = 'running'; $('span', rows[state.completedStages]).textContent = '进行中'; return; }
    clearInterval(state.timer); state.timer = null; state.running = false;
    $('#execution-title').textContent = '任务完成'; $('#execution-detail').textContent = `${rows.length} 个检查步骤均已完成`;
    $('#stop-button').hidden = true; restoreRunButton(); renderResult();
  }, state.tool === 'cf-optimize' ? 520 : 420);
  $('#stop-button').hidden = false;
  setTimeout(() => $('#execution-panel').scrollIntoView({ behavior: 'smooth', block: 'nearest' }), 0);
}

function restoreRunButton() {
  $('#run-button').classList.remove('danger'); $('#run-button').classList.add('primary'); $('#reset-button').disabled = false;
  $('#run-button').textContent = state.tool === 'monitor' ? '再次监控' : state.tool === 'cf-optimize' ? '重新优选' : state.tool === 'cf-status' ? '刷新状态' : '重新检查';
}

function stopTask(notify) {
  if (state.timer) clearInterval(state.timer);
  if (state.inlineMonitorTimer) clearTimeout(state.inlineMonitorTimer);
  state.timer = null;
  state.inlineMonitorTimer = null;
  if (state.running && notify) {
    $('#execution-title').textContent = '任务已停止'; $('#execution-detail').textContent = state.tool === 'monitor' ? '已采集的监控证据仍会保存到本地历史' : '未完成的临时结果不会写入历史';
    showToast(state.tool === 'monitor' ? '监控已停止，已保存当前证据' : '任务已停止');
  }
  state.running = false;
  if ($('#run-button')) restoreRunButton();
  if ($('#stop-button')) $('#stop-button').hidden = true;
}

function metrics(items) { return `<div class="metric-grid">${items.map(([label, value, note = '']) => `<div><span>${label}</span><strong>${value}</strong><small>${note}</small></div>`).join('')}</div>`; }
function findings(items) { return `<div class="finding-list">${items.map(([title, copy, status = 'good']) => `<article class="${status}"><i></i><div><strong>${title}</strong><p>${copy}</p></div><em>${status === 'warning' ? '需关注' : '正常'}</em></article>`).join('')}</div>`; }
function actions(items) { return `<div class="action-list">${items.map(([title, copy, jump, status = 'warning']) => `<article class="${status}"><i></i><div><strong>${title}</strong><p>${copy}</p></div><button type="button" data-result-jump="${jump}">前往处理</button></article>`).join('')}</div>`; }
function dataTable(headers, rows, cols = 8) { return `<div class="table-wrap"><div class="data-table"><div class="data-row head cols-${cols}">${headers.map(item => `<span>${item}</span>`).join('')}</div>${rows.map(row => `<div class="data-row cols-${cols}">${row.map(item => `<span>${item}</span>`).join('')}</div>`).join('')}</div></div>`; }
function resultDetails(title, summary, content, id = '') { return `<details class="result-details" ${id ? `id="${id}"` : ''}><summary><span><strong>${title}</strong><small>${summary}</small></span><em>展开</em></summary><div class="result-details-body">${content}</div></details>`; }
function siteDiagnosticModules() { return `<section class="diagnostic-overview"><header><div><strong>诊断模块</strong><span>一次检查已经覆盖全部能力</span></div><small>4 MODULES</small></header><div class="diagnostic-module-list">
  <article class="warning"><i>DNS</i><div><strong>域名解析</strong><p>系统 DNS 响应偏慢，公共解析器正常</p></div><b>184 ms</b><button type="button" data-site-detail="dns">查看证据</button></article>
  <article><i>WEB</i><div><strong>端口与 Web 服务</strong><p>80 / 443 可达，TLS 1.3 与 HTTP/2 正常</p></div><b>正常</b><button type="button" data-site-detail="service">查看证据</button></article>
  <article><i>RT</i><div><strong>网络链路</strong><p>12 跳到达 Cloudflare NRT，未发现中断</p></div><b>46 ms</b><button type="button" data-site-detail="route">查看证据</button></article>
  <article class="warning"><i>PX</i><div><strong>连接路径</strong><p>强制直连比系统代理快 38%</p></div><b>推荐直连</b><button type="button" data-site-detail="path">查看证据</button></article>
</div></section>`; }
function inlineMonitorPanel() { return `<section class="inline-monitor" id="inline-monitor" hidden><header><div><strong>持续观察网站稳定性</strong><p>沿用本次目标和连接策略，按间隔执行小流量采样。</p></div><button class="button primary compact" type="button" data-result-action="start-inline-monitor">开始监控</button></header><div class="form-grid"><div class="field"><label>监控时长</label><div class="control"><select id="inline-monitor-duration"><option selected>30 秒</option><option>2 分钟</option><option>5 分钟</option><option>15 分钟</option></select></div></div></div>${advancedSettings('监控设置', '默认每秒采样', `<div class="form-grid three"><div class="field"><label>采样间隔</label><div class="control"><select><option selected>1 秒</option><option>2 秒</option><option>5 秒</option><option>10 秒</option></select></div></div><div class="field"><label>延迟阈值</label><div class="control"><select><option>300 ms</option><option selected>500 ms</option><option>1000 ms</option></select></div></div><div class="field"><label>失败率阈值</label><div class="control"><select><option>1%</option><option>5%</option><option selected>10%</option></select></div></div></div>`)}<div class="inline-monitor-result" id="inline-monitor-result" hidden></div></section>`; }
function routeWorkbench() { return `<section class="route-workbench"><header><strong>网络链路详情</strong><div class="mini-tabs"><button class="active" type="button" data-route-view="map">地域链路</button><button type="button" data-route-view="raw">原始跳点</button></div></header><div class="route-view" data-route-panel="map"><div class="route-map"><div class="map-controls"><button type="button" data-map-action="zoom-in" title="放大地图">+</button><button type="button" data-map-action="zoom-out" title="缩小地图">−</button><button type="button" data-map-action="fit" title="适配完整链路">⌖</button></div><div class="route-stage"><i class="route-line"></i><button class="map-node" style="left:9%;top:110px" data-label="本机" data-node-detail="第 1 跳 · 192.168.1.1 · 本地网关 · 1 ms">1</button><button class="map-node" style="left:31%;top:82px" data-label="上海 · 电信" data-node-detail="第 4 跳 · 202.101.63.18 · 上海电信 · 8 ms">4</button><button class="map-node" style="left:58%;top:59px" data-label="东京 · 骨干网" data-node-detail="第 8 跳 · 203.181.106.14 · Tokyo · 41 ms · GeoIP 估算">8</button><button class="map-node" style="left:84%;top:76px" data-label="NRT · Cloudflare" data-node-detail="第 12 跳 · 104.16.123.96 · Cloudflare NRT · 46 ms">12</button></div><aside class="route-node-detail">点击节点查看跳点详情</aside></div></div><div class="route-view route-raw" data-route-panel="raw" hidden><div class="route-filters"><button class="active" type="button" data-hop-filter="all">ALL</button><button type="button" data-hop-filter="reply">RESPONDED</button><button type="button" data-hop-filter="timeout">NO REPLY</button></div><div class="route-hop" data-hop-status="reply"><b>1</b><span>192.168.1.1</span><span>本地网关</span><span>1 ms</span></div><div class="route-hop" data-hop-status="reply"><b>4</b><span>202.101.63.18</span><span>上海电信</span><span>8 ms</span></div><div class="route-hop" data-hop-status="timeout"><b>6</b><span>* * *</span><span>NO REPLY</span><span>--</span></div><div class="route-hop" data-hop-status="reply"><b>8</b><span>203.181.106.14</span><span>Tokyo</span><span>41 ms</span></div><div class="route-hop" data-hop-status="reply"><b>12</b><span>104.16.123.96</span><span>Cloudflare NRT</span><span>46 ms</span></div><p class="field-message">NO REPLY 只表示该跳点未响应探测，不等同于链路中断。</p></div></section>`; }

function renderResult() {
  const tool = state.tool;
  let title = '检查完成', description = '已形成本次任务的诊断证据与建议', content = '', buttons = [['复制报告', 'copy'], ['保存到历史', 'save']];
  if (tool === 'network-full') {
    title = '当前网络整体可用，DNS 响应略慢'; description = '12 个站点中 11 个正常，国际站点首包耗时偏高。';
    content = `${metrics([['活动接口','Wi-Fi','MTU 1500'],['IPv4 出口','可用','电信'],['IPv6 出口','不可用','协议栈未建立'],['站点通过','11 / 12','91.7%'],['共性问题','DNS','P95 468 ms']])}<div class="result-grid"><section class="result-section"><div class="section-title"><strong>分层结论</strong><span>NETWORK BASELINE</span></div>${findings([['默认路由稳定','未发现多出口竞争或网关漂移'],['DNS 响应偏慢','系统 DNS P95 高于公共 DNS','warning'],['代理环境已识别','Clash / Mihomo + 活动 TUN']])}</section><section class="result-section"><div class="section-title"><strong>治理顺序</strong><span>REMEDIATION</span></div>${actions([['切换公共 DNS 后复测','先验证解析耗时是否改善','site-dns'],['检查国际站点路径','对比系统代理与强制直连','site-path']])}</section></div>${resultDetails('详细证据', '12 个站点的分段耗时与连接路径', dataTable(['站点','状态','DNS','TCP','TLS','TTFB','总耗时','连接路径'],[['Cloudflare','正常','18 ms','42 ms','61 ms','126 ms','248 ms','系统代理'],['百度','正常','9 ms','18 ms','--','42 ms','88 ms','直连'],['GitHub','正常','22 ms','71 ms','93 ms','214 ms','438 ms','系统代理'],['YouTube','超时','16 ms','--','--','--','--','系统代理']],8))}`;
  } else if (tool === 'baseline') {
    title = '网络基线采集完成'; description = '检测到活动 TUN，IPv4 出口正常，IPv6 尚未建立。';
    content = `${metrics([['活动接口','wlan0','Wi-Fi'],['默认路由','192.168.1.1','MTU 1500'],['IPv4','可用','电信出口'],['IPv6','不可用','无默认路由'],['本地代理','127.0.0.1:7890','HTTP / SOCKS5']])}<section class="result-section result-primary-section"><div class="section-title"><strong>本地网络建议</strong><span>LOCAL ACTIONS</span></div>${actions([['优先处理系统 DNS','公共解析器响应更稳定','site-dns'],['确认 TUN 接管范围','直连测试仍可能经过 TUN','site-path']])}</section>${resultDetails('详细证据', '系统 DNS 与 3 个公共解析器对照', findings([['系统 DNS','104.16.123.96 · 184 ms','warning'],['Cloudflare DNS','104.16.123.96 · 38 ms'],['AliDNS','104.16.124.96 · 42 ms'],['Google DoH','104.16.123.96 · 64 ms']]))}`;
  } else if (tool === 'patrol') {
    title = '4 个站点完成巡检'; description = '3 个正常，YouTube 连接超时；其余站点未发现共性故障。';
    content = `${metrics([['目标','4','已完成'],['通过','3','75%'],['异常','1','连接超时'],['平均总耗时','286 ms','正常'],['连接路径','2 类','直连 / 代理']])}${resultDetails('详细证据', '4 个站点的 DNS、连接与响应耗时', dataTable(['站点','状态','DNS','TCP','TLS','TTFB','总耗时','连接路径'],[['Cloudflare','正常','18 ms','42 ms','61 ms','126 ms','248 ms','系统代理'],['百度','正常','9 ms','18 ms','--','42 ms','88 ms','直连'],['GitHub','正常','22 ms','71 ms','93 ms','214 ms','438 ms','系统代理'],['YouTube','超时','16 ms','--','--','--','--','系统代理']],8))}`;
  } else if (tool === 'site-full') {
    title = '网站可以访问，DNS 响应偏慢'; description = 'TLS、HTTP 和 Cloudflare 边缘节点正常，系统代理路径比直连慢 244 ms。';
    content = `${metrics([['DNS','184 ms','偏慢'],['TCP','42 ms','正常'],['TLS','61 ms','TLS 1.3'],['TTFB','126 ms','正常'],['总耗时','438 ms','系统代理']])}${siteDiagnosticModules()}<section class="monitor-prompt"><div><strong>需要确认问题是否偶发？</strong><p>继续观察会沿用当前网站与连接策略，不需要重新填写目标。</p></div><button class="button secondary compact" type="button" data-result-action="site-monitor">持续观察</button></section>${inlineMonitorPanel()}`+
      `${resultDetails('DNS 深度证据', '系统 DNS、公共 UDP 与 DoH 对照', dataTable(['解析器','传输','耗时','响应码','A / AAAA','CNAME','TTL','判断'],[['系统 DNS','LOCAL','184 ms','NOERROR','104.16.123.96','--','260','偏慢'],['Cloudflare','UDP','38 ms','NOERROR','104.16.124.96','--','300','正常'],['AliDNS','UDP','42 ms','NOERROR','104.16.123.96','--','300','正常'],['Google','DoH','64 ms','NOERROR','104.16.124.96','--','278','正常']],8), 'site-detail-dns')}`+
      `${resultDetails('端口与 Web 服务证据', 'TCP、HTTP、TLS、证书与 ALPN', `${findings([['HTTPS 服务正常','HTTP 200 · TLS 1.3 · h2'],['证书有效','Cloudflare Inc ECC CA-3 · 还有 74 天'],['Cloudflare 节点正常','CF-Ray NRT · DYNAMIC']])}${dataTable(['端口','服务','地址','耗时','协议','HTTP','TLS / ALPN','状态'],[['80','HTTP','104.16.123.96','41 ms','HTTP','301','--','开放'],['443','HTTPS','104.16.123.96','43 ms','TLS','200','TLS 1.3 / h2','开放']],8)}`, 'site-detail-service')}`+
      `${resultDetails('网络链路证据', '地域链路、原始跳点与无响应筛选', routeWorkbench(), 'site-detail-route')}`+
      `${resultDetails('连接路径对照', '系统代理、强制直连与手动代理', dataTable(['路径','实际出口','成功率','TCP P50','TLS P50','TTFB P50','总耗时 P50','抖动'],[['系统代理','Clash / TUN','100%','76 ms','92 ms','286 ms','642 ms','84 ms'],['强制直连','电信 IPv4','100%','42 ms','61 ms','126 ms','398 ms','28 ms'],['手动代理','127.0.0.1:7890','100%','64 ms','81 ms','202 ms','518 ms','57 ms']],8), 'site-detail-path')}`;
  } else if (tool === 'dns') {
    title = '系统 DNS 响应偏慢，未发现污染证据'; description = '解析地址差异符合 CDN 调度特征，公共 DNS 与 DoH 均可用。';
    content = `${metrics([['解析器','5','均有响应'],['系统 DNS','184 ms','最慢'],['最快','Cloudflare UDP','38 ms'],['记录组','3','A / AAAA / CNAME'],['DNSSEC','AD','Google DoH']])}${resultDetails('详细证据', '解析器、传输、记录、TTL 与 DNSSEC', dataTable(['解析器','传输','耗时','响应码','A / AAAA','CNAME','TTL','判断'],[['系统 DNS','LOCAL','184 ms','NOERROR','104.16.123.96','--','260','偏慢'],['Cloudflare','UDP','38 ms','NOERROR','104.16.124.96','--','300','正常'],['AliDNS','UDP','42 ms','NOERROR','104.16.123.96','--','300','正常'],['Google','DoH','64 ms','NOERROR','104.16.124.96','--','278','正常']],8))}`;
  } else if (tool === 'ports') {
    title = '2 个端口均可达'; description = '80 返回重定向，443 的 TLS、证书和 HTTP/2 均正常。';
    content = `${metrics([['开放','2','80 / 443'],['拒绝','0','--'],['超时','0','--'],['解析 IP','2','A 记录'],['协议证据','完整','HTTP + TLS']])}${resultDetails('详细证据', '逐端口地址、协议与握手结果', dataTable(['端口','服务','地址','耗时','协议','HTTP','TLS / ALPN','状态'],[['80','HTTP','104.16.123.96','41 ms','HTTP','301','--','开放'],['443','HTTPS','104.16.123.96','43 ms','TLS','200','TLS 1.3 / h2','开放']],8))}`;
  } else if (tool === 'monitor') {
    title = '30 秒监控完成，出现 1 次延迟超限'; description = '可用率 100%，未发生路径变化，P95 受一次高延迟样本影响。';
    content = `${metrics([['可用率','100%','30 / 30'],['P50','214 ms','正常'],['P95','684 ms','偏高'],['Jitter','92 ms','需关注'],['路径变化','0','稳定']])}${resultDetails('详细证据', '30 次采样趋势与 1 个阈值事件', `<div class="trend-chart" style="height:130px"><i style="height:35%"></i><i style="height:42%"></i><i style="height:38%"></i><i style="height:81%;background:var(--amber)"></i><i style="height:43%"></i><i style="height:39%"></i><i style="height:41%"></i></div><div class="finding-list" style="margin-top:10px"><article class="warning"><i></i><div><strong>14:32:18 · 延迟超过 500 ms</strong><p>总耗时 684 ms，远端 IP 与 Cloudflare 节点未变化。</p></div><em>已恢复</em></article></div>`)}`;
  } else if (tool === 'path') {
    title = '强制直连是当前推荐路径'; description = '三次采样成功率均为 100%，直连总耗时 P50 比系统代理低 38%。';
    content = `${metrics([['推荐路径','强制直连','398 ms'],['系统代理','642 ms','P50'],['手动代理','518 ms','P50'],['成功率','100%','全部路径'],['抖动','28 ms','直连']])}${resultDetails('详细证据', '3 条连接路径的分段耗时与抖动', dataTable(['路径','实际出口','成功率','TCP P50','TLS P50','TTFB P50','总耗时 P50','抖动'],[['系统代理','Clash / TUN','100%','76 ms','92 ms','286 ms','642 ms','84 ms'],['强制直连','电信 IPv4','100%','42 ms','61 ms','126 ms','398 ms','28 ms'],['手动代理','127.0.0.1:7890','100%','64 ms','81 ms','202 ms','518 ms','57 ms']],8))}`;
  } else if (tool === 'cf-optimize') {
    title = '已找到适用于当前域名的推荐 IP'; description = '推荐节点已通过 Host、SNI、TLS、状态码与 CF-Ray 验证。'; buttons = [['复制完整报告','copy'],['复制域名配置','config'],['导出 CSV','export'],['生成全局配置','bundle']];
    content = `${metrics([['候选总数','256','IPv4'],['可响应','241','94.1%'],['通过筛选','18','延迟 / 丢包'],['推荐 IP','104.18.7.12','HKG'],['下载速度','28.6 MB/s','10 秒']])}<section class="cdn-comparison"><div class="section-title"><strong>当前 DNS 与推荐 IP</strong><span>同一路径同参数对比</span></div><div class="cdn-node-flow"><article><span>当前 DNS IP</span><strong>104.18.6.12</strong><small>86 ms · 1.2% 丢包 · 17.8 MB/s</small></article><i>→</i><article class="recommended"><span>推荐 IP</span><strong>104.18.7.12</strong><small>54 ms · 0% 丢包 · 28.6 MB/s</small></article></div><div class="cdn-uplift"><div><span>延迟变化</span><strong>降低 37%</strong><small>86 → 54 ms</small></div><div><span>丢包变化</span><strong>减少 1.2%</strong><small>1.2% → 0%</small></div><div><span>速度变化</span><strong>提升 60.7%</strong><small>17.8 → 28.6 MB/s</small></div></div></section><section class="acceleration-banner" id="result-acceleration"><i></i><div><strong>${state.cfActive ? 'Cloudflare IP 优选已应用' : '可以应用本次推荐 IP'}</strong><p>${state.cfActive ? 'CF 流量使用 104.18.7.12，其他流量保持原出口；原系统代理恢复点已保存。' : '内置兼容网关只接管当前域名，可随时停止并恢复原系统代理。'}</p></div><button class="button ${state.cfActive ? 'danger' : 'primary'} compact" data-result-action="${state.cfActive ? 'rollback' : 'apply'}" type="button">${state.cfActive ? '停止并恢复' : '应用优选'}</button></section>${resultDetails('详细证据', '18 个入围节点的延迟、丢包、速度与验证状态', dataTable(['排名','IP 地址','已发送','已接收','丢包率','平均延迟','下载速度','地区码','CDN / 状态'],[['1','104.18.7.12','4','4','0%','54 ms','28.6 MB/s','HKG','Cloudflare · 200'],['2','104.17.22.31','4','4','0%','58 ms','26.9 MB/s','HKG','Cloudflare · 200'],['3','104.16.18.90','4','4','0%','61 ms','24.2 MB/s','NRT','Cloudflare · 200']],9))}`;
  } else {
    title = state.cfActive ? '加速网关运行正常' : '当前未启用 Cloudflare 加速'; description = '状态已从本机兼容网关和系统代理恢复点刷新。';
    content = `${metrics([['状态',state.cfActive ? '运行中' : '未启用','本地网关'],['目标域名',state.cfActive ? 'example.com' : '--','仅当前域名'],['推荐 IP',state.cfActive ? '104.18.7.12' : '--','HKG'],['监听端点',state.cfActive ? '127.0.0.1:17891' : '--','HTTP / HTTPS'],['恢复点',state.cfActive ? '已保存' : '无','系统代理']])}`;
  }
  $('#result-title').textContent = title; $('#result-description').textContent = description; $('#result-content').innerHTML = content;
  $('#result-actions').innerHTML = buttons.map(([label, action]) => `<button class="button secondary" type="button" data-result-action="${action}">${label}</button>`).join('');
  $('#result-panel').hidden = false; bindResultInteractions();
  $('#result-panel').scrollIntoView({ behavior: 'smooth', block: 'start' });
}

function bindResultInteractions() {
  bindRouteWorkbench($('#result-content'));
  $$('[data-site-detail]').forEach(button => button.addEventListener('click', () => {
    const detail = $(`#site-detail-${button.dataset.siteDetail}`);
    if (!detail) return;
    detail.open = true;
    detail.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
  }));
  $$('[data-result-jump]').forEach(button => button.addEventListener('click', () => jumpTo(button.dataset.resultJump)));
  $$('[data-result-action]').forEach(button => button.addEventListener('click', () => handleResultAction(button.dataset.resultAction)));
}

function handleResultAction(action) {
  if (action === 'site-monitor') {
    const panel = $('#inline-monitor');
    panel.hidden = false;
    panel.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
    return showToast('已沿用本次网站和连接策略');
  }
  if (action === 'start-inline-monitor') {
    const button = $('[data-result-action="start-inline-monitor"]');
    const result = $('#inline-monitor-result');
    if (state.inlineMonitorTimer) clearTimeout(state.inlineMonitorTimer);
    button.disabled = true; button.textContent = '监控中'; result.hidden = false;
    result.innerHTML = `<div class="inline-monitor-running"><i></i><div><strong>正在采样</strong><p>${$('#inline-monitor-duration').value} · 每秒一次 · 已保存当前路径基线</p></div></div>`;
    state.inlineMonitorTimer = setTimeout(() => {
      state.inlineMonitorTimer = null; button.disabled = false; button.textContent = '再次监控';
      result.innerHTML = `${metrics([['可用率','100%','30 / 30'],['P95','684 ms','1 次超限'],['路径变化','0','稳定']])}<div class="finding-list"><article class="warning"><i></i><div><strong>出现 1 次延迟超限</strong><p>14:32:18 总耗时 684 ms，随后恢复；远端 IP 与节点未变化。</p></div><em>已恢复</em></article></div>`;
      showToast('持续监控完成，证据已加入本次报告');
    }, 900);
    return;
  }
  if (action === 'apply') return confirmAction('应用 Cloudflare 推荐 IP？', '内置兼容网关只接管当前域名的系统 HTTP / HTTPS 代理流量，并保存原设置用于恢复。', '应用优选', () => { state.cfActive = true; showToast('CF IP 优选已应用，恢复点已保存'); renderResult(); $('#recovery-banner').hidden = false; });
  if (action === 'rollback') return confirmAction('停止并恢复原系统代理？', '确认当前设置未被其他程序修改后，系统将停止网关并恢复应用前的代理设置。', '停止并恢复', () => { state.cfActive = false; showToast('已停止加速并恢复原系统代理'); renderResult(); $('#recovery-banner').hidden = true; });
  if (action === 'save') {
    const meta = toolMeta[state.tool];
    historyRecords.unshift({ id: Date.now(), scene: state.scene, tool: state.tool, type: `${sceneMeta[state.scene].title} · ${meta.label}`, target: state.scene === 'network' ? '当前设备网络' : state.scene === 'cloudflare' ? 'example.com' : 'www.cloudflare.com', time: '刚刚', metric: $('#result-title').textContent, finding: '本次结果' });
    updateHistoryCount();
    return showToast('本次结果已保存到本地历史');
  }
  const messages = { copy: '报告已复制，敏感代理凭据已遮盖', config: '当前域名配置已复制', export: 'CSV 已导出到本地', bundle: '全局加速配置包已生成' };
  showToast(messages[action] || '操作已完成');
}

function jumpTo(target) {
  const siteIntent = { 'site-dns': '网站检查已包含 DNS 深度对照', 'site-path': '网站检查已包含连接路径对照', 'site-monitor': '完成网站检查后可继续持续观察' };
  if (siteIntent[target]) { openScene('site', 'site-full'); return showToast(siteIntent[target]); }
  const map = { site: ['site','site-full'], cloudflare: ['cloudflare','cf-optimize'] };
  if (map[target]) openScene(...map[target]);
}

function openDrawer(name) {
  $('#drawer-layer').hidden = false; $('#history-drawer').hidden = name !== 'history'; $('#governance-drawer').hidden = name !== 'governance';
  if (name === 'history') renderHistory();
}
function closeDrawer() { $('#drawer-layer').hidden = true; $('#history-drawer').hidden = true; $('#governance-drawer').hidden = true; }

function renderHistory() {
  updateHistoryCount();
  $('#history-list').innerHTML = historyRecords.map(record => `<article class="history-row"><input type="checkbox" value="${record.id}" aria-label="选择记录"><div><strong>${record.type}</strong><span>${record.target} · ${record.metric} · ${record.finding}</span></div><div><small>${record.time}</small><div class="history-actions"><button type="button" data-history-action="retest" data-id="${record.id}">复测</button><button type="button" data-history-action="export" data-id="${record.id}">导出 ZIP</button></div></div></article>`).join('');
  $$('input[type="checkbox"]', $('#history-list')).forEach(input => input.addEventListener('change', updateHistorySelection));
  $$('[data-history-action]').forEach(button => button.addEventListener('click', () => {
    const record = historyRecords.find(item => item.id === Number(button.dataset.id));
    if (button.dataset.historyAction === 'retest') { closeDrawer(); openScene(record.scene, record.tool); showToast('已沿用历史配置，可直接重新检查'); }
    else showToast(`${record.target} 的脱敏 ZIP 报告已导出`);
  }));
}
function updateHistorySelection() {
  const selected = $$('input:checked', $('#history-list'));
  if (selected.length > 2) { this.checked = false; return showToast('最多选择两条记录进行对比'); }
  if (selected.length === 2) {
    const records = selected.map(input => historyRecords.find(item => item.id === Number(input.value)));
    if (records[0]?.tool !== records[1]?.tool) { this.checked = false; $('#compare-history').disabled = true; return showToast('请选择两条同类型记录进行对比'); }
  }
  $('#compare-history').disabled = selected.length !== 2;
}
function updateHistoryCount() { $('#history-count').textContent = String(historyRecords.length); }
function compareHistory() {
  $('#history-comparison').hidden = false;
  $('#history-comparison').innerHTML = `<h3>治理前后指标对比</h3><div class="comparison-grid"><div><span>总耗时 P50</span><strong class="positive">612 → 438 ms</strong></div><div><span>TTFB P50</span><strong class="positive">286 → 126 ms</strong></div><div><span>推荐路径</span><strong>系统代理 → 直连</strong></div></div>`;
}

function renderImport(tab = 'overview') {
  const content = $('#import-content');
  if (tab === 'overview') content.innerHTML = `${metrics([['结论','可访问','WARNING'],['总耗时','486 ms',''],['目标','api.example.com','']])}<p class="field-message" style="margin-top:10px">离线报告包含 8 个经过校验的证据文件，未发现格式损坏。</p>`;
  else if (tab === 'evidence') content.innerHTML = findings([['本地网络','默认路由与 DNS 已采集'],['服务链路','TLS 1.3 · HTTP 200'],['代理路径','系统代理比直连慢 22%','warning']]);
  else if (tab === 'route') content.innerHTML = routeWorkbench();
  else content.innerHTML = `<pre style="margin:0;max-height:180px;overflow:auto;color:var(--soft);font-size:7px">{\n  "schema_version": 2,\n  "target": "api.example.com",\n  "status": "warning",\n  "files_validated": 8\n}</pre>`;
  bindRouteWorkbench(content);
}

function bindRouteWorkbench(root) {
  $$('.route-workbench', root).forEach(workbench => {
    $$('[data-route-view]', workbench).forEach(button => button.addEventListener('click', () => {
      $$('[data-route-view]', workbench).forEach(item => item.classList.toggle('active', item === button));
      $$('[data-route-panel]', workbench).forEach(panel => panel.hidden = panel.dataset.routePanel !== button.dataset.routeView);
    }));
    const stage = $('.route-stage', workbench);
    let scale = 1, offsetX = 0, offsetY = 0, drag = null;
    const applyTransform = () => { if (stage) stage.style.transform = `translate(${offsetX}px, ${offsetY}px) scale(${scale})`; };
    $$('[data-map-action]', workbench).forEach(button => button.addEventListener('click', () => {
      if (button.dataset.mapAction === 'fit') { scale = 1; offsetX = 0; offsetY = 0; }
      else scale = Math.max(.75, Math.min(1.75, scale + (button.dataset.mapAction === 'zoom-in' ? .25 : -.25)));
      applyTransform();
    }));
    $$('.map-node', workbench).forEach(node => node.addEventListener('click', event => { event.stopPropagation(); $('.route-node-detail', workbench).textContent = node.dataset.nodeDetail; }));
    const map = $('.route-map', workbench);
    map?.addEventListener('pointerdown', event => { if (event.target.closest('button')) return; drag = { x: event.clientX, y: event.clientY, offsetX, offsetY }; map.setPointerCapture(event.pointerId); });
    map?.addEventListener('pointermove', event => { if (!drag) return; offsetX = drag.offsetX + event.clientX - drag.x; offsetY = drag.offsetY + event.clientY - drag.y; applyTransform(); });
    map?.addEventListener('pointerup', () => { drag = null; });
    $$('[data-hop-filter]', workbench).forEach(button => button.addEventListener('click', () => {
      $$('[data-hop-filter]', workbench).forEach(item => item.classList.toggle('active', item === button));
      $$('.route-hop', workbench).forEach(row => row.hidden = button.dataset.hopFilter !== 'all' && row.dataset.hopStatus !== button.dataset.hopFilter);
    }));
  });
}

function confirmAction(title, message, label, action) {
  $('#modal-title').textContent = title; $('#modal-message').textContent = message; $('#modal-confirm').textContent = label; state.modalAction = action; $('#modal-layer').hidden = false;
}
function closeModal() { $('#modal-layer').hidden = true; state.modalAction = null; }
function showToast(message) { clearTimeout(state.toastTimer); $('#toast').textContent = message; $('#toast').hidden = false; state.toastTimer = setTimeout(() => $('#toast').hidden = true, 2400); }

function bindGlobalInteractions() {
  $$('[data-open-scene]').forEach(button => button.addEventListener('click', () => openScene(button.dataset.openScene)));
  $('#brand-home').addEventListener('click', openHome); $('#back-button').addEventListener('click', openHome);
  $('#tool-form').addEventListener('submit', startTask); $('#stop-button').addEventListener('click', () => stopTask(true)); $('#reset-button').addEventListener('click', () => selectTool(state.tool));
  $('#tool-info').addEventListener('click', () => showToast(toolMeta[state.tool].boundary));
  $('#scene-history').addEventListener('click', () => openDrawer('history'));
  $$('[data-open-drawer]').forEach(button => button.addEventListener('click', () => openDrawer(button.dataset.openDrawer)));
  $('#drawer-backdrop').addEventListener('click', closeDrawer); $$('[data-close-drawer]').forEach(button => button.addEventListener('click', closeDrawer));
  $('#history-tabs').addEventListener('click', event => { const button = event.target.closest('[data-history-tab]'); if (!button) return; $$('[data-history-tab]').forEach(item => item.classList.toggle('active', item === button)); $$('[data-history-view]').forEach(view => view.hidden = view.dataset.historyView !== button.dataset.historyTab); });
  $('#compare-history').addEventListener('click', compareHistory);
  $('#clear-history').addEventListener('click', () => confirmAction('清空全部本地历史？', '此操作会移除本机保存的检测记录，但不会删除已经导出的 ZIP 报告。', '清空历史', () => { historyRecords.splice(0); renderHistory(); $('#history-comparison').hidden = true; showToast('本地历史已清空'); }));
  $('#import-report').addEventListener('click', () => { $('#import-preview').hidden = false; renderImport(); showToast('报告包已在本机完成校验'); });
  $('#close-import').addEventListener('click', () => $('#import-preview').hidden = true);
  $$('#import-preview [data-import-tab]').forEach(button => button.addEventListener('click', () => { $$('#import-preview [data-import-tab]').forEach(item => item.classList.toggle('active', item === button)); renderImport(button.dataset.importTab); }));
  $('#save-import').addEventListener('click', () => { historyRecords.unshift({ id: Date.now(), scene: 'site', tool: 'site-full', type: '导入报告 · 网站检查', target: 'api.example.com', time: '刚刚', metric: '总耗时 486 ms', finding: '离线导入' }); updateHistoryCount(); showToast('导入报告已保存到本地历史'); }); $('#compare-import').addEventListener('click', () => showToast('已生成导入报告与本机记录的差异摘要'));
  $$('[data-governance-jump]').forEach(button => button.addEventListener('click', () => { closeDrawer(); jumpTo(button.dataset.governanceJump); }));
  $('#rollback-button').addEventListener('click', () => confirmAction('停止并恢复原系统代理？', '只有当前设置仍与加速状态一致时才会执行恢复。', '停止并恢复', () => { state.cfActive = false; $('#recovery-banner').hidden = true; showToast('原系统代理已恢复'); }));
  $('#modal-cancel').addEventListener('click', closeModal); $('#modal-confirm').addEventListener('click', () => { const action = state.modalAction; closeModal(); action?.(); });
  $('#theme-button').addEventListener('click', () => { const dark = document.body.dataset.theme !== 'dark'; document.body.dataset.theme = dark ? 'dark' : 'light'; $('#theme-button').title = dark ? '切换亮色主题' : '切换深色主题'; showToast(dark ? '已切换深色主题' : '已切换亮色主题'); });
  $('#language-button').addEventListener('click', event => { event.currentTarget.textContent = event.currentTarget.textContent === 'EN' ? '中' : 'EN'; showToast(event.currentTarget.textContent === '中' ? 'Prototype language: English' : '原型语言：简体中文'); });
  $('#version-button').addEventListener('click', () => { $('#update-banner').hidden = false; $('#update-title').textContent = '发现新版本 0.9.5'; $('#update-detail').textContent = '修复代理兼容问题并更新 Cloudflare 网段'; });
  $('#update-later').addEventListener('click', () => $('#update-banner').hidden = true);
  $('#update-action').addEventListener('click', startUpdate);
  document.addEventListener('keydown', event => { if (event.key === 'Escape') { if (!$('#modal-layer').hidden) closeModal(); else if (!$('#drawer-layer').hidden) closeDrawer(); } });
}

function startUpdate() {
  const button = $('#update-action');
  if (button.textContent === '重启安装') return showToast('应用将在关闭后安装更新');
  button.disabled = true; $('#update-progress').hidden = false; $('#update-title').textContent = '正在下载更新'; let progress = 0;
  const timer = setInterval(() => { progress += 20; $('#update-progress-bar').style.width = `${progress}%`; $('#update-detail').textContent = `${progress}% · 已验证签名`; if (progress < 100) return; clearInterval(timer); button.disabled = false; button.textContent = '重启安装'; $('#update-title').textContent = '更新已准备完成'; $('#update-detail').textContent = '重启应用后完成安装'; }, 180);
}

bindGlobalInteractions();
renderHistory();
openHome();
