export function summariseNetworkOverview({
  baseline,
  patrol,
  baselineIssues = [],
  patrolIssues = [],
  selectedCount = 0,
  successCount = 0,
}) {
  const incompleteIssues = [];
  if (!baseline || baseline.status !== 'completed') {
    incompleteIssues.push({
      status: 'warning',
      title: '网络基线未完整完成',
      detail: '未能获得完整的本机网络结果，请重新检查。',
    });
  }
  if (!patrol || patrol.status !== 'completed') {
    incompleteIssues.push({
      status: 'warning',
      title: '常用站点巡检未完整完成',
      detail: '未能完成全部站点巡检，请重新检查。',
    });
  }

  const issues = [...incompleteIssues, ...baselineIssues, ...patrolIssues];
  const hasCritical =
    baselineIssues.some((item) => item.status === 'critical') ||
    (Array.isArray(patrol?.results) &&
      patrol.results.length > 0 &&
      patrol.results.every((item) => !item.result?.success));
  const incomplete = incompleteIssues.length > 0;
  const hasWarning =
    incomplete ||
    baselineIssues.some((item) => item.status === 'warning') ||
    patrolIssues.length > 0;

  return {
    level: hasCritical ? 'critical' : hasWarning ? 'warning' : 'healthy',
    headline: hasCritical
      ? '当前网络存在阻断问题'
      : incomplete
        ? '整体检查未完整完成'
        : hasWarning
          ? '整体网络可用，但存在需要关注的项目'
          : '当前网络与常用站点状态正常',
    detail: `${successCount} / ${selectedCount} 个站点通过巡检`,
    incomplete,
    issues,
  };
}
