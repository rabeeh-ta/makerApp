// The GIFs app's page: your GIFs, their order, and how long each one plays.
import { useEffect, useState } from 'react'
import { get, post, upload } from '../lib/api'

type Item = { Hash: string; Name: string; Hold: number; Bytes: number }
type List = { Items: Item[]; Used: number; Room: number; Max: number }

const kb = (n: number) => `${Math.ceil(n / 1024)} KB`

export function GifManager() {
  const [list, setList] = useState<List | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    get<List>('/api/gifs').then(setList, (e: Error) => setError(e.message))
  }, [])

  function run(request: Promise<List>) {
    setBusy(true)
    setError('')
    request
      .then(setList)
      .catch((e: Error) => setError(e.message))
      .finally(() => setBusy(false))
  }

  function add(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0]
    e.target.value = '' // so the same file can be picked again
    if (file) run(upload<List>('/api/gifs', file))
  }

  function save(items: Item[]) {
    run(post<List>('/api/gifs/save', { items: JSON.stringify(items.map(({ Hash, Hold }) => ({ Hash, Hold }))) }))
  }

  function move(i: number, by: number) {
    if (!list) return
    const items = [...list.Items]
    const [it] = items.splice(i, 1)
    items.splice(i + by, 0, it)
    save(items)
  }

  if (!list) {
    return <section className="card">{error ? <p className="warn small">{error}</p> : <p className="muted small">Loading...</p>}</section>
  }
  const full = list.Items.length >= list.Max
  return (
    <section className="card stack">
      <h3>Your GIFs</h3>
      {list.Items.length === 0 && <p className="muted small">None yet. Add one, then install the app on your board.</p>}
      <ul className="gif-rows">
        {list.Items.map((it, i) => (
          <li className="gif-row" key={it.Hash}>
            <img src={`/anims/${it.Hash}.gif`} alt="" />
            <span className="gif-name" title={it.Name}>
              {it.Name}
              <span className="muted small"> {kb(it.Bytes)}</span>
            </span>
            <label className="gif-hold small">
              Plays for
              {/* Remounts with the saved value, so a time the server corrected shows. */}
              <input
                key={it.Hold}
                type="number"
                min={2}
                max={300}
                defaultValue={it.Hold}
                aria-label={`Seconds ${it.Name} plays for`}
                disabled={busy}
                onBlur={(e) => {
                  const hold = Number(e.target.value)
                  if (hold !== it.Hold) save(list.Items.map((x) => (x.Hash === it.Hash ? { ...x, Hold: hold } : x)))
                }}
              />
              s
            </label>
            <button type="button" className="btn ghost small" aria-label={`Move ${it.Name} up`} disabled={busy || i === 0} onClick={() => move(i, -1)}>
              Up
            </button>
            <button type="button" className="btn ghost small" aria-label={`Move ${it.Name} down`} disabled={busy || i === list.Items.length - 1} onClick={() => move(i, 1)}>
              Down
            </button>
            <button type="button" className="btn ghost small" aria-label={`Remove ${it.Name}`} disabled={busy} onClick={() => save(list.Items.filter((x) => x.Hash !== it.Hash))}>
              Remove
            </button>
          </li>
        ))}
      </ul>
      <label>
        {full ? `That's ${list.Max} GIFs, the most it holds` : 'Add a GIF'}
        <input type="file" accept="image/gif" onChange={add} disabled={busy || full} />
      </label>
      {busy && <p className="muted small">Saving...</p>}
      {error && <p className="warn small">{error}</p>}
      <div className="stack">
        <div className="gif-meter" role="img" aria-label={`${kb(list.Used)} of ${kb(list.Room)} used`}>
          <i style={{ width: `${Math.min(100, (list.Used / list.Room) * 100)}%` }} />
        </div>
        <p className="muted small">
          {kb(list.Used)} of {kb(list.Room)} on the board. Games you install use this space too. GIFs are cropped to fit, in 15 colors, and long ones lose frames.
        </p>
      </div>
    </section>
  )
}
