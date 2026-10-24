// Collapsible JSON tree. In "copy" mode a click copies a value's {{ path }};
// in "insert" mode it hands the path to onPick (the screen editor's fields).
import { useMemo, useRef } from 'react'
import { toast } from '../lib/toast'

type Mode = 'copy' | 'insert'

const SPECIAL = /[.*?#|@\\]/g
const esc = (k: string) => k.replace(SPECIAL, (c) => '\\' + c) // gjson path escaping
const join = (p: string, k: string) => (p ? p + '.' + k : k)

export function JsonTree({ json, mode, onPick }: { json: string; mode: Mode; onPick?: (path: string) => void }) {
  const box = useRef<HTMLDivElement>(null)
  const data = useMemo(() => {
    try {
      return { ok: true, v: JSON.parse(json) as unknown }
    } catch {
      return { ok: false, v: null }
    }
  }, [json])
  const setAll = (open: boolean) => box.current?.querySelectorAll('details').forEach((d) => (d.open = open))
  return (
    <>
      <div className="tree-tools">
        <button type="button" onClick={() => setAll(true)}>Expand all</button>
        <button type="button" onClick={() => setAll(false)}>Collapse all</button>
      </div>
      <div className="json-tree" ref={box}>
        {data.ok ? <Node k={null} v={data.v} path="" depth={0} mode={mode} onPick={onPick} /> : 'Not JSON'}
      </div>
    </>
  )
}

function Node({ k, v, path, depth, mode, onPick }: { k: string | null; v: unknown; path: string; depth: number; mode: Mode; onPick?: (path: string) => void }) {
  if (v !== null && typeof v === 'object') {
    const isArr = Array.isArray(v)
    const keys = isArr ? v.map((_, i) => String(i)) : Object.keys(v)
    const limit = 50
    return (
      <details open={depth < 2}>
        <summary>
          {k !== null && <span className="k">{k}</span>}
          <span className="meta">{isArr ? `[${keys.length}]` : `{${keys.length}}`}</span>
          {path && mode === 'insert' && (
            <button
              type="button"
              className="path pick"
              title={`Use this ${isArr ? 'list' : 'object'}`}
              onClick={(e) => {
                e.preventDefault() // pick the list without toggling it
                onPick?.(path)
              }}
            >
              ⤶
            </button>
          )}
        </summary>
        <div className="kids">
          {keys.slice(0, limit).map((key) => (
            <Node
              key={key}
              k={key}
              v={(v as Record<string, unknown>)[key]}
              path={join(path, isArr ? key : esc(key))}
              depth={depth + 1}
              mode={mode}
              onPick={onPick}
            />
          ))}
          {keys.length > limit && <div className="more">… {keys.length - limit} more</div>}
        </div>
      </details>
    )
  }
  const tpl = '{{ ' + path + ' }}'
  const inner = (
    <>
      <span className="k">{k}</span>
      <Leaf v={v} />
    </>
  )
  if (mode === 'insert') {
    return (
      <button type="button" className="leaf path" title={'Insert ' + tpl} onClick={() => onPick?.(path)}>
        {inner}
      </button>
    )
  }
  return (
    <div
      className="leaf copyable"
      title={'Copy ' + tpl}
      onClick={() => navigator.clipboard?.writeText(tpl).then(() => toast('Copied ' + tpl), () => toast(tpl))}
    >
      {inner}
    </div>
  )
}

function Leaf({ v }: { v: unknown }) {
  if (v === null) return <span className="v t-null">null</span>
  if (typeof v === 'string') return <span className="v t-str">"{v.length > 80 ? v.slice(0, 80) + '…' : v}"</span>
  if (typeof v === 'number') return <span className="v t-num">{v}</span>
  return <span className="v t-bool">{String(v)}</span>
}
