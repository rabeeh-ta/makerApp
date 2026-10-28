import { useState } from 'react'
import { DeviceHead, ErrorNotice, Loading, Notice } from '../components/common'
import { Thumb } from '../components/DeviceScreen'
import { cycleName, useTitle } from '../lib/format'
import { useData, useForm, useLocation } from '../lib/router'
import type { DeviceView, Screen } from '../lib/types'

type Choice = { App: string; AppName: string; Screen: string; Preview: Screen }

type Data = {
  Device: DeviceView
  IntervalSec: number
  Items: { app: string; screen: string }[] | null
  Choices: Choice[]
  After: number
  Intervals: number[]
  Runs: boolean // the board's firmware plays a screensaver
}

const key = (app: string, screen: string) => `${app}/${screen}`

// DeviceScreensaver is the device's Screensaver tab: which screens of its
// installed apps it plays when it sits on the Apps drawer, in what order, and
// how long each stays.
export function DeviceScreensaver({ id }: { id: string }) {
  const { query } = useLocation()
  const { data, error, stamp } = useData<Data>(`/api/devices/${id}/screensaver`)
  useTitle(data && `${data.Device.Name} · Screensaver`)
  if (!data) return <Loading error={error} />
  return (
    <>
      <DeviceHead device={data.Device} tab="screensaver" />
      {query.has('saved') && <Notice>Saved. The board has the new screensaver.</Notice>}
      {!data.Runs && <Notice warn>The screensaver needs firmware 3.2. The board installs it by itself the next time it's online.</Notice>}
      <Editor key={stamp} data={data} />
    </>
  )
}

function Editor({ data }: { data: Data }) {
  const choices = new Map(data.Choices.map((c) => [key(c.App, c.Screen), c]))
  const [items, setItems] = useState(() => (data.Items ?? []).map((it) => key(it.app, it.screen)).filter((k) => choices.has(k)))
  const { error, busy, submit } = useForm()
  const move = (i: number, d: number) =>
    setItems((list) => {
      const next = [...list]
      ;[next[i], next[i + d]] = [next[i + d], next[i]]
      return next
    })
  const available = data.Choices.filter((c) => !items.includes(key(c.App, c.Screen)))
  return (
    <form onSubmit={submit(`/api/devices/${data.Device.ID}/screensaver`)} className="stack">
      <p className="muted small">
        When the board sits on its Apps drawer for {data.After} seconds, it plays these screens one after another until you press a button. Games
        aren't included: they need the buttons.
      </p>
      <ErrorNotice error={error} />
      {items.map((k) => (
        <input type="hidden" name="item" value={k} key={k} />
      ))}
      <div className="grid two wide-left">
        <section className="card stack">
          <div className="row between">
            <h3>In the screensaver</h3>
            <label className="row saver-interval">
              <span className="muted small">Each screen for</span>
              <select name="interval_sec" defaultValue={data.IntervalSec}>
                {data.Intervals.map((s) => (
                  <option key={s} value={s}>
                    {cycleName(s).replace('Every ', '')}
                  </option>
                ))}
              </select>
            </label>
          </div>
          {items.length ? (
            items.map((k, i) => {
              const c = choices.get(k)!
              return (
                <div className="saver-row" key={k}>
                  <Thumb s={c.Preview} empty="" />
                  <div>
                    <strong>{c.Preview.name || c.Preview.title || c.AppName}</strong>
                    <div className="muted small">{c.AppName}</div>
                  </div>
                  <div className="row">
                    <button type="button" className="btn ghost small icon" disabled={i === 0} onClick={() => move(i, -1)} title="Play earlier">
                      ↑
                    </button>
                    <button type="button" className="btn ghost small icon" disabled={i === items.length - 1} onClick={() => move(i, 1)} title="Play later">
                      ↓
                    </button>
                    <button type="button" className="btn ghost small" onClick={() => setItems(items.filter((x) => x !== k))}>
                      Remove
                    </button>
                  </div>
                </div>
              )
            })
          ) : (
            <p className="muted small">No screens yet, so the board stays on the Apps drawer. Add some from the list.</p>
          )}
          <div className="row between">
            <span className="muted small">{items.length ? `${items.length} screens, then it starts over.` : ''}</span>
            <button className="btn" disabled={busy}>
              Save screensaver
            </button>
          </div>
        </section>

        <section className="card stack">
          <h3>Screens you can add</h3>
          {available.length ? (
            available.map((c) => (
              <div className="saver-row" key={key(c.App, c.Screen)}>
                <Thumb s={c.Preview} empty="" />
                <div>
                  <strong>{c.Preview.name || c.Preview.title || c.AppName}</strong>
                  <div className="muted small">{c.AppName}</div>
                </div>
                <button type="button" className="btn ghost small" onClick={() => setItems([...items, key(c.App, c.Screen)])}>
                  Add
                </button>
              </div>
            ))
          ) : (
            <p className="muted small">
              {data.Choices.length ? 'Every screen is in the screensaver.' : 'Install an app with screens (not a game) to use them here.'}
            </p>
          )}
        </section>
      </div>
    </form>
  )
}
