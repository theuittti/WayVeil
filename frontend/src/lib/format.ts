const units = ['B', 'KB', 'MB', 'GB', 'TB']

export function bytes(n: number): string {
  if (!Number.isFinite(n) || n <= 0) return '0 B'
  let i = 0
  let v = n
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  if (i === 0) return `${Math.round(v)} B`
  return `${v < 100 ? v.toFixed(1) : Math.round(v)} ${units[i]}`
}

const p2 = (n: number) => String(n).padStart(2, '0')

export function duration(ms: number): string {
  const t = Number.isFinite(ms) && ms > 0 ? Math.floor(ms / 1000) : 0
  const h = Math.floor(t / 3600)
  const m = Math.floor((t % 3600) / 60)
  const s = t % 60
  return h > 0 ? `${h}:${p2(m)}:${p2(s)}` : `${m}:${p2(s)}`
}
