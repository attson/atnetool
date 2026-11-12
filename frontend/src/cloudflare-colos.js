const COLO_NAMES = {
  AMS: { zh: '阿姆斯特丹', en: 'Amsterdam' },
  ARN: { zh: '斯德哥尔摩', en: 'Stockholm' },
  BKK: { zh: '曼谷', en: 'Bangkok' },
  BLR: { zh: '班加罗尔', en: 'Bengaluru' },
  BOM: { zh: '孟买', en: 'Mumbai' },
  CAN: { zh: '广州', en: 'Guangzhou' },
  CDG: { zh: '巴黎', en: 'Paris' },
  CPT: { zh: '开普敦', en: 'Cape Town' },
  DEL: { zh: '德里', en: 'Delhi' },
  DFW: { zh: '达拉斯', en: 'Dallas' },
  DXB: { zh: '迪拜', en: 'Dubai' },
  EWR: { zh: '纽瓦克', en: 'Newark' },
  FRA: { zh: '法兰克福', en: 'Frankfurt' },
  GRU: { zh: '圣保罗', en: 'Sao Paulo' },
  HEL: { zh: '赫尔辛基', en: 'Helsinki' },
  HKG: { zh: '香港', en: 'Hong Kong' },
  IAD: { zh: '华盛顿', en: 'Washington' },
  ICN: { zh: '首尔', en: 'Seoul' },
  JNB: { zh: '约翰内斯堡', en: 'Johannesburg' },
  KIX: { zh: '大阪', en: 'Osaka' },
  LAX: { zh: '洛杉矶', en: 'Los Angeles' },
  LHR: { zh: '伦敦', en: 'London' },
  MAD: { zh: '马德里', en: 'Madrid' },
  MEL: { zh: '墨尔本', en: 'Melbourne' },
  MEX: { zh: '墨西哥城', en: 'Mexico City' },
  MNL: { zh: '马尼拉', en: 'Manila' },
  MRS: { zh: '马赛', en: 'Marseille' },
  MXP: { zh: '米兰', en: 'Milan' },
  NRT: { zh: '东京', en: 'Tokyo' },
  ORD: { zh: '芝加哥', en: 'Chicago' },
  PEK: { zh: '北京', en: 'Beijing' },
  SEA: { zh: '西雅图', en: 'Seattle' },
  SIN: { zh: '新加坡', en: 'Singapore' },
  SJC: { zh: '圣何塞', en: 'San Jose' },
  SYD: { zh: '悉尼', en: 'Sydney' },
  TPE: { zh: '台北', en: 'Taipei' },
  WAW: { zh: '华沙', en: 'Warsaw' },
  YVR: { zh: '温哥华', en: 'Vancouver' },
  YYZ: { zh: '多伦多', en: 'Toronto' },
};

export function formatCloudflareColo(value, locale = 'zh-CN') {
  const code = String(value || '').trim().toUpperCase();
  if (!code) return '--';
  const english = locale === 'en-US';
  const name = COLO_NAMES[code]?.[english ? 'en' : 'zh'];
  return name ? (english ? `${name} (${code})` : `${name}（${code}）`) : code;
}
