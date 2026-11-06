export function getCFAccelerationBlockReason(result) {
  if (!result?.best_ip) return '请先完成 CF IP 优选';
  if (!result?.target_verification?.verified) {
    return result?.target_verification?.message || '目标域名尚未确认使用 Cloudflare 代理，请检查代理状态后重新测试';
  }
  if (!result?.target_validated) {
    const detail = result?.target_validation || '推荐 IP 尚未通过目标域名验证';
    return `${detail}；请切换到 HTTPing 后重新测试`;
  }
  return '';
}

export function getCDNGovernanceBlockReason(result) {
  const accelerationReason = getCFAccelerationBlockReason(result);
  if (accelerationReason) return accelerationReason;
  if (result?.effective_route === 'proxy') {
    return '代理出口结果不能生成直连治理配置，请切换本机网络后重新测试';
  }
  return '';
}
