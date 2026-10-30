// Browser replica of what the 240x135 board draws.
import type { CSSProperties } from 'react'
import { sparkPoints } from '../lib/format'
import type { Screen } from '../lib/types'

export function DeviceScreen({ s, big }: { s: Screen; big?: boolean }) {
  const style = { ...(s.bg && { '--lcd-bg': s.bg }), ...(s.accent && { '--accent': s.accent }) } as CSSProperties
  return (
    <>
      <div className={big ? 'lcd lcd-big' : 'lcd'} style={style}>
        {s.layout !== 'gif' && <div className="lcd-title">{s.title}</div>}
        <Body s={s} />
      </div>
      {big && s.error && <p className="warn small">Source error: {s.error}</p>}
    </>
  )
}

function Body({ s }: { s: Screen }) {
  switch (s.layout) {
    case 'gif':
      return s.gif ? <img className="lcd-gif" src={`/anims/${s.gif}.gif`} alt="" /> : <div className="lcd-text">No GIF yet</div>
    case 'list':
      return (
        <div className="lcd-list">
          {s.items?.length ? (
            s.items.slice(0, 5).map(([label, value], i) => (
              <div className="lcd-row" key={i}>
                <span>{label}</span>
                <b>{value}</b>
              </div>
            ))
          ) : (
            <div className="lcd-dim">No items</div>
          )}
        </div>
      )
    case 'text':
      return <div className="lcd-text">{s.body}</div>
    case 'spark':
      return (
        <>
          <div className="lcd-value lcd-value-sm">{s.value}</div>
          {s.series?.length ? (
            <svg className="lcd-spark" viewBox="-1 -2 226 64" preserveAspectRatio="none">
              <polyline points={sparkPoints(s.series)} />
            </svg>
          ) : (
            <div className="lcd-dim">No series data</div>
          )}
        </>
      )
    case 'clock':
      return (
        <>
          <div className="lcd-clock">{s.value}</div>
          <div className="lcd-sub">{s.sub}</div>
        </>
      )
    case 'progress':
      return (
        <>
          <div className="lcd-value">{s.value}</div>
          <div className="lcd-bar">
            <i style={{ width: `${((s.pct ?? 0) * 100).toFixed(1)}%` }} />
          </div>
          <div className="lcd-sub">{s.sub}</div>
        </>
      )
  }
  const art = appArt[s.layout]
  if (art) return art
  return (
    <>
      <div className="lcd-value">{s.value}</div>
      <div className="lcd-sub" style={s.subColor ? { color: s.subColor } : undefined}>
        {s.sub}
      </div>
    </>
  )
}

// Thumb is a screen in a card, or a placeholder when there's none.
export function Thumb({ s, empty }: { s: Screen | null | undefined; empty: string }) {
  return (
    <div className="device-thumb">
      {s ? (
        <DeviceScreen s={s} />
      ) : (
        <div className="lcd lcd-empty">
          <span>{empty}</span>
        </div>
      )}
    </div>
  )
}

const holes = (top: number) => [35, 105, 175].map((x) => <i className="hole" key={`${x}-${top}`} style={{ left: x, top }} />)

// Still frames of the catalog's Lua apps, keyed by app ID (their preview's layout).
const appArt: Record<string, React.ReactNode> = {
  pomodoro: (
    <div className="lcd-art">
      <span className="art-score" style={{ color: '#f54e00', textShadow: 'none' }}>Focus</span>
      <b className="dot on" style={{ left: 192 }} />
      <b className="dot on" style={{ left: 204 }} />
      <b className="dot" style={{ left: 216 }} />
      <b className="dot" style={{ left: 228 }} />
      <span className="art-timer">18:42</span>
      <i className="track" />
      <i className="track fill" style={{ width: 62 }} />
      <span className="art-hint">Middle: pause &nbsp; Bottom: skip</span>
    </div>
  ),
  flappy: (
    <div className="lcd-art art-flappy">
      <i className="pipe" style={{ left: 104, top: 0, height: 30 }} />
      <i className="pipe" style={{ left: 104, bottom: 0, height: 40 }} />
      <i className="pipe" style={{ left: 196, top: 0, height: 52 }} />
      <i className="pipe" style={{ left: 196, bottom: 0, height: 18 }} />
      <b className="hog" style={{ left: 35, top: 60 }} />
      <span className="art-score">Score: 3</span>
    </div>
  ),
  paddle: (
    <div className="lcd-art">
      <i className="net" />
      <i className="bar" style={{ left: 5, top: 42, height: 30 }} />
      <i className="bar" style={{ left: 230, top: 64, height: 30 }} />
      <b className="ball" style={{ left: 150, top: 72 }} />
      <span className="art-score big" style={{ left: 90 }}>2</span>
      <span className="art-score big" style={{ left: 140 }}>1</span>
    </div>
  ),
  runner: (
    <div className="lcd-art">
      <i className="ground" />
      <b className="hog" style={{ left: 30, top: 105, width: 18, height: 13 }} />
      <i className="cactus" style={{ left: 118, height: 24 }} />
      <i className="cactus" style={{ left: 186, height: 16, width: 14 }} />
      <i className="bird" style={{ left: 210, top: 98 }} />
      <span className="art-score" style={{ left: 'auto', right: 8 }}>00042</span>
    </div>
  ),
  snake: (
    <div className="lcd-art">
      <span className="art-score small">Score 7</span>
      <i className="rule" style={{ top: 14 }} />
      <i className="seg" style={{ left: 96, top: 64, width: 46 }} />
      <i className="seg" style={{ left: 96, top: 64, height: 38 }} />
      <i className="seg head" style={{ left: 136, top: 64 }} />
      <b className="food" style={{ left: 184, top: 65 }} />
    </div>
  ),
  breakout: (
    <div className="lcd-art">
      {[168, 180, 192, 204, 216].map((x) => (
        <i className="bricks" key={x} style={{ left: x }} />
      ))}
      <i className="bar" style={{ left: 6, top: 54, height: 28 }} />
      <b className="ball" style={{ left: 92, top: 40 }} />
    </div>
  ),
  whack: (
    <div className="lcd-art art-grass">
      <span className="art-score small">Time 21</span>
      {holes(42)}
      {/* Drawn before the lower holes, so the hog pops out of one. */}
      <b className="hog" style={{ left: 110, top: 66, width: 18, height: 18 }} />
      {holes(82)}
      {holes(122)}
    </div>
  ),
  simon: (
    <div className="lcd-art">
      <span className="art-score small">Round 4</span>
      <i className="pad" style={{ top: 18, background: '#4a1714' }} />
      <i className="pad" style={{ top: 58, background: '#2bb673' }} />
      <i className="pad" style={{ top: 98, background: '#10284f' }} />
    </div>
  ),
  stacker: (
    <div className="lcd-art">
      <span className="art-score small">Rows 4</span>
      <i className="row" style={{ left: 96, top: 56, width: 30, background: '#fff' }} />
      <i className="row" style={{ left: 96, top: 92, width: 46, background: '#f58a00' }} />
      <i className="row" style={{ left: 80, top: 101, width: 62, background: '#f57000' }} />
      <i className="row" style={{ left: 80, top: 110, width: 78, background: '#f55e00' }} />
      <i className="row" style={{ left: 64, top: 119, width: 78, background: '#f54e00' }} />
    </div>
  ),
  shooter: (
    <div className="lcd-art art-stars">
      <i className="ship" style={{ top: 60 }} />
      <i className="shot" style={{ left: 60, top: 65 }} />
      {[150, 168, 186].map((x) => (
        <i className="aliens" key={x} style={{ left: x, top: 22 }} />
      ))}
      <span className="art-score small">Score 120</span>
    </div>
  ),
}
