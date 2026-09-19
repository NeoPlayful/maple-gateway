// shortId 把 ID 去连字符后截取前 12 位，用于界面统一显示短 ID。
// 与后端 pkg.ShortID 语义一致：UUID 去掉横线再取前 12 位（如 574aa2d4d469）。
// 空值返回 ''。仅用于展示，底层仍是完整 UUID，复制/接口调用仍用原值。
export function shortId(id?: string | null): string {
  if (!id) return '';
  const s = id.replace(/-/g, '');
  return s.length > 12 ? s.slice(0, 12) : s;
}
