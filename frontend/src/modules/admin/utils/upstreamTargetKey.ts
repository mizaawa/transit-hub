/**
 * 上游分组（站点 + 分组名）的复合 Map key。
 *
 * 必须全局唯一实现：这个 key 由「分组关联」面板构建 Map、由「自动调价」抽屉查询 Map，
 * 两侧一旦各写一份就会格式不一致。历史上面板用 `\u0000` 拼接、抽屉用 `::` 拼接，
 * 于是抽屉永远查不到倍率，界面上明明有倍率却提示「暂无可用上游倍率数据，无法计算预估倍率」。
 *
 * 用 \u0000 而不是 `::` 作分隔符：分组名允许包含冒号，`a::b` + `c` 与 `a` + `b::c`
 * 会拼出同一个 key 造成串味；U+0000 不可能出现在合法的站点 ID 或分组名里。
 */
export const upstreamTargetKey = (siteId: string, groupName: string): string =>
  `${siteId}\u0000${groupName}`

/** 从复合 key 还原出站点 ID 与分组名。分组名本身含分隔符时也能正确还原。 */
export const parseUpstreamTargetKey = (key: string): { siteId: string; groupName: string } => {
  const index = key.indexOf('\u0000')
  if (index < 0) return { siteId: key, groupName: '' }
  return { siteId: key.slice(0, index), groupName: key.slice(index + 1) }
}
