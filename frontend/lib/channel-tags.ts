/**
 * channelTagKey 返回标签的比较键：只把 ASCII 的 A-Z 转成小写，其余字符原样保留。
 *
 * 与后端 storage.NormalizeChannelTags（lowerASCII）的去重规则一致：
 * "VIP" 与 "vip" 视为同一个标签，"Ärger" 与 "ärger" 则是两个不同的标签。
 * 不要换成 toLowerCase()：它会折叠全部 Unicode 大小写，把后端认为不同的标签合并成一个。
 */
export function channelTagKey(tag: string): string {
  return tag.replace(/[A-Z]+/g, (s) => s.toLowerCase())
}
