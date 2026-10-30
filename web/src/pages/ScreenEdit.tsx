import { Fragment, useRef, useState } from 'react'
import { ErrorNotice, Loading, Notice } from '../components/common'
import { DeviceScreen } from '../components/DeviceScreen'
import { JsonTree } from '../components/JsonTree'
import { get, post, upload } from '../lib/api'
import { ago, layoutName, layouts, useTitle } from '../lib/format'
import { Link, PostButton, useData, useForm, useLocation } from '../lib/router'
import type { App, Device, Screen, Source, Spec, StoredScreen } from '../lib/types'

type Explorer = { Source: Source | null; Paths: unknown }
type Data = {
  Showing: (Device & { Online: boolean })[] | null
  App: App | null
  Apps: App[] | null
  Screen: StoredScreen
  Spec: Spec
  Sources: Source[] | null
  Preview: Screen
  Explorer: Explorer
  DeviceFields: { Name: string; Desc: string }[]
}

export function ScreenEdit({ id }: { id: string }) {
  const { query } = useLocation()
  const path = id ? `/api/screens/${id}` : `/api/screens/new?${new URLSearchParams({ app: query.get('app') ?? '', source: query.get('source') ?? '' })}`
  const { data, error, stamp } = useData<Data>(path)
  useTitle(id ? data?.Screen.Name : 'New screen')
  if (!data) return <Loading error={error} />
  return <Editor key={stamp} data={data} saved={query.has('saved')} />
}

const T = '{{'
const E = '}}'

function Editor({ data, saved }: { data: Data; saved: boolean }) {
  const sc = data.Screen
  const sp = data.Spec
  const form = useRef<HTMLFormElement>(null)
  const lastField = useRef<HTMLInputElement | HTMLTextAreaElement | null>(null)
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  const [layout, setLayout] = useState(sc.Layout)
  const [preview, setPreview] = useState(data.Preview)
  const [explorer, setExplorer] = useState(data.Explorer)
  const { error, busy, submit } = useForm()
  const [gif, setGif] = useState(sp.gif ?? '')
  const [gifNote, setGifNote] = useState('')
  const [gifError, setGifError] = useState('')
  const [converting, setConverting] = useState(false)

  function uploadGif(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0]
    if (!file) return
    setConverting(true)
    setGifError('')
    setGifNote('')
    upload<{ hash: string; bytes: number }>('/api/animations', file)
      .then((r) => {
        setGif(r.hash)
        setGifNote(`Converted: ${Math.ceil(r.bytes / 1024)} KB on the board.`)
        schedulePreview(50)
      })
      .catch((err: Error) => setGifError(err.message))
      .finally(() => setConverting(false))
  }

  function refreshPreview() {
    if (!form.current) return
    post<Screen>('/api/screens/preview', new FormData(form.current)).then(setPreview, () => {})
  }
  // Typing waits 300 ms for a pause; picks and toggles update at once.
  function schedulePreview(delay = 300) {
    clearTimeout(timer.current)
    timer.current = setTimeout(refreshPreview, delay)
  }

  // Remember the template field focused last, so clicks on fields insert there.
  function rememberField(e: React.FocusEvent) {
    const t = e.target
    if ((t instanceof HTMLInputElement || t instanceof HTMLTextAreaElement) && t.hasAttribute('data-tpl')) lastField.current = t
  }
  function insert(path: string) {
    const field = lastField.current ?? form.current?.querySelector<HTMLInputElement>('[data-tpl]')
    if (!field) return
    if (field.hasAttribute('data-raw')) {
      field.value = path
    } else {
      const ins = `${T} ${path} ${E}`
      const start = field.selectionStart ?? field.value.length
      const end = field.selectionEnd ?? field.value.length
      field.value = field.value.slice(0, start) + ins + field.value.slice(end)
      field.selectionStart = field.selectionEnd = start + ins.length
    }
    field.focus()
    schedulePreview(0)
  }
  function pickSource(sourceID: string) {
    get<Explorer>(`/api/screens/explorer?source_id=${encodeURIComponent(sourceID)}`).then(setExplorer, () => {})
    schedulePreview(0)
  }
  const show = (...ls: string[]) => !ls.includes(layout)

  return (
    <>
      <div className="page-head">
        <div>
          <p className="crumbs">
            <Link to="/apps">My apps</Link> /
            {data.App && (
              <>
                {' '}
                <Link to={`/apps/${data.App.ID}`}>{data.App.Name}</Link> /
              </>
            )}
          </p>
          <h1>{sc.ID ? sc.Name : 'New screen'}</h1>
        </div>
        {sc.ID && (
          <PostButton action={`/api/screens/${sc.ID}/delete`} className="btn danger" confirm="Delete this screen?">
            Delete
          </PostButton>
        )}
      </div>
      <ErrorNotice error={error} />
      {saved && (
        <Notice>
          {/* One span: the notice is a flex row, which would space out each piece. */}
          <span>
            Saved.
            {data.Showing?.map((d, i) => (
              <Fragment key={d.ID}>
                {i ? ', ' : ' '}
                <b>{d.Name}</b> {d.Online ? 'updated now' : '(offline, it updates when it reconnects)'}
              </Fragment>
            ))}
            {data.Showing?.length ? '.' : ''}
          </span>
        </Notice>
      )}

      <div className="grid two editor">
        <form
          ref={form}
          id="screen-form"
          className="stack"
          onSubmit={submit(sc.ID ? `/api/screens/${sc.ID}` : '/api/screens')}
          onInput={() => schedulePreview()}
          onChange={() => schedulePreview(0)}
          onFocus={rememberField}
        >
          <section className="card stack">
            {!sc.ID && (
              <label>
                App
                <select name="app" defaultValue={sc.AppID || 'new'}>
                  {(data.Apps ?? []).map((a) => (
                    <option key={a.ID} value={a.ID}>
                      {a.Name}
                    </option>
                  ))}
                  <option value="new">+ New app, named after this screen</option>
                </select>
              </label>
            )}
            <label>
              Screen name
              <input name="name" defaultValue={sc.Name} placeholder="e.g. Signups" />
            </label>
            <fieldset className="layouts">
              <legend>Layout</legend>
              {layouts.map((l) => (
                <label className="layout-opt" key={l}>
                  <input type="radio" name="layout" value={l} checked={layout === l} onChange={() => setLayout(l)} />
                  <span className={`lo lo-${l}`} />
                  {layoutName(l)}
                </label>
              ))}
            </fieldset>
          </section>

          <section className="card stack" hidden={show('gif')}>
            <h3>GIF</h3>
            <input type="hidden" name="gif" value={gif} />
            <label>
              {gif ? 'Replace the GIF' : 'Pick a GIF'}
              <input type="file" accept="image/gif" onChange={uploadGif} disabled={converting} />
            </label>
            {converting && <p className="muted small">Converting...</p>}
            {gifNote && <p className="muted small">{gifNote}</p>}
            {gifError && <p className="warn small">{gifError}</p>}
            <p className="muted small">It fills the screen, cropped to fit, in 15 colors. Long GIFs lose frames to fit the board.</p>
          </section>

          <section className="card stack" hidden={layout === 'gif'}>
            <h3>Content</h3>
            <p className="muted small">
              Type text or click a field on the right.{' '}
              <span title="| num  | compact  | money  | pct  | pct100  | fixed:1  | sum  | len  | ago  | date   ·   @ago = when fetched" className="hint">
                Formats
              </span>
            </p>
            <div className="row">
              <label className="grow">
                Title
                <input name="title" defaultValue={sp.title} data-tpl="" />
              </label>
              <label className="shrink">
                Accent
                <input type="color" name="accent" defaultValue={sp.accent || '#F54E00'} />
              </label>
              <label className="shrink">
                Background
                <input type="color" name="bg" defaultValue={sp.bg || '#000000'} />
              </label>
            </div>
            <div hidden={show('big_number', 'spark', 'progress')}>
              <label>
                Main value
                <input name="value" defaultValue={sp.value} data-tpl="" placeholder={`${T} results.0.0 | compact ${E}`} />
              </label>
            </div>
            <div hidden={show('big_number', 'progress')}>
              <label>
                Subtitle
                <input name="sub" defaultValue={sp.sub} data-tpl="" placeholder={`updated ${T} @ago ${E}`} />
              </label>
            </div>
            <div hidden={show('big_number')}>
              <label>
                Subtitle color
                <select name="sub_color" defaultValue={sp.sub_color}>
                  <option value="">Gray</option>
                  <option value="auto">Green if it starts with +, red if it starts with -</option>
                  <option value="#2BB673">Green</option>
                  <option value="#F04438">Red</option>
                  <option value="#F54E00">Orange</option>
                </select>
              </label>
            </div>
            <div hidden={show('text')}>
              <label>
                Text
                <textarea name="body" rows={5} defaultValue={sp.body} data-tpl="" />
              </label>
            </div>
            <div className="stack" hidden={show('list')}>
              <label>
                List
                <input name="items_path" defaultValue={sp.items_path} data-tpl="" data-raw="" />
              </label>
              <div className="row">
                <label className="grow">
                  Row label
                  <input name="item_label" defaultValue={sp.item_label} data-tpl="" placeholder={`${T} properties.dealname ${E}`} />
                </label>
                <label className="grow">
                  Row value
                  <input name="item_value" defaultValue={sp.item_value} data-tpl="" placeholder={`${T} properties.amount | money ${E}`} />
                </label>
                <label className="shrink">
                  Max rows
                  <input type="number" name="max_items" defaultValue={sp.max_items} min={1} max={30} />
                </label>
              </div>
            </div>
            <div hidden={show('spark')}>
              <label>
                Series
                <input name="series_path" defaultValue={sp.series_path} data-tpl="" data-raw="" />
              </label>
            </div>
            <div className="row" hidden={show('progress')}>
              <label className="grow">
                Progress value
                <input name="pct_value" defaultValue={sp.pct_value} data-tpl="" placeholder={`${T} total ${E}`} />
              </label>
              <label className="grow">
                Out of (target)
                <input name="pct_max" defaultValue={sp.pct_max} data-tpl="" placeholder="100" />
              </label>
            </div>
          </section>

          <div className="savebar">
            <button className="btn btn-lg" type="submit" disabled={busy}>
              Save
            </button>
          </div>
        </form>

        <aside className="stack sticky">
          <section className="card">
            <h3>Preview</h3>
            <div>
              <DeviceScreen s={preview} big />
            </div>
          </section>
          <section className="card explorer">
            <div className="device-fields">
              <div className="row between">
                <strong className="small">This device</strong>
                <span className="muted small" title="The board fills these in itself, so the clock keeps ticking even offline.">
                  live on the board
                </span>
              </div>
              <div className="chips">
                {data.DeviceFields.map((f) => (
                  <button type="button" className="path chip" key={f.Name} title={`e.g. ${f.Desc}`} onClick={() => insert(`device.${f.Name}`)}>
                    {f.Name}
                  </button>
                ))}
              </div>
            </div>
            {/* Part of the form via form=, so it saves and refreshes the preview like the fields on the left. */}
            <label className="source-pick">
              Data source
              <select name="source_id" form="screen-form" defaultValue={sc.SourceID} onChange={(e) => pickSource(e.target.value)}>
                <option value="">None (fixed text)</option>
                {(data.Sources ?? []).map((s) => (
                  <option key={s.ID} value={s.ID}>
                    {s.Name}
                  </option>
                ))}
              </select>
            </label>
            <div>
              {explorer.Source ? (
                <>
                  <div className="row between">
                    <strong className="small">Fields</strong>
                    <span className="muted small">fetched {ago(explorer.Source.LastFetchedAt)}</span>
                  </div>
                  <p className="muted small">Click a value to insert it.</p>
                  {explorer.Source.LastData ? (
                    <JsonTree key={explorer.Source.ID} json={explorer.Source.LastData} mode="insert" onPick={insert} />
                  ) : (
                    <p className="muted small">No data yet.</p>
                  )}
                </>
              ) : (
                <p className="muted small">Pick a source above to see its fields.</p>
              )}
            </div>
          </section>
        </aside>
      </div>
    </>
  )
}
