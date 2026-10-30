import { useEffect } from 'react'

export function useTitle(title: string | undefined) {
  useEffect(() => {
    document.title = title ? `${title} · makerApp` : 'makerApp'
  }, [title])
}

export function ago(unix: number): string {
  if (!unix) return 'never'
  const s = Date.now() / 1000 - unix
  if (s < 60) return 'just now'
  if (s < 3600) return `${Math.floor(s / 60)}m ago`
  if (s < 48 * 3600) return `${Math.floor(s / 3600)}h ago`
  return `${Math.floor(s / 86400)}d ago`
}

// count shortens big numbers: 999, 1.2k, 3.4M.
export function count(n: number): string {
  if (n < 1000) return String(n)
  if (n < 1e6) return (n / 1000).toFixed(1).replace(/\.0$/, '') + 'k'
  return (n / 1e6).toFixed(1).replace(/\.0$/, '') + 'M'
}

export function plural(n: number, word: string): string {
  return `${n} ${word}${n === 1 ? '' : 's'}`
}

export const layouts = ['big_number', 'list', 'text', 'spark', 'progress', 'gif']

export function layoutName(l: string): string {
  return ({ big_number: 'Big number', list: 'List', text: 'Text', spark: 'Sparkline', progress: 'Progress', gif: 'GIF' } as Record<string, string>)[l] ?? ''
}

export function cycleName(sec: number): string {
  if (sec === 0) return 'Off'
  if (sec < 60) return `Every ${sec} seconds`
  if (sec === 60) return 'Every minute'
  return `Every ${sec / 60} minutes`
}

// sparkPoints maps a series to SVG polyline points in a 224x60 box.
export function sparkPoints(series: number[]): string {
  if (series.length < 2) return ''
  let lo = Math.min(...series)
  let hi = Math.max(...series)
  if (hi - lo < 1e-9) hi = lo + 1
  return series.map((v, i) => `${((i * 224) / (series.length - 1)).toFixed(1)},${(60 - ((v - lo) / (hi - lo)) * 60).toFixed(1)}`).join(' ')
}
