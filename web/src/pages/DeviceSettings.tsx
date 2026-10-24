import { useEffect, useState, type FormEvent } from 'react'
import { DeviceHead, ErrorNotice, Loading, Notice } from '../components/common'
import { post } from '../lib/api'
import { useTitle } from '../lib/format'
import { navigate, useData, useForm, useLocation } from '../lib/router'
import type { DeviceData } from './Device'

export function DeviceSettings({ id }: { id: string }) {
  const { query } = useLocation()
  const { data, error, stamp } = useData<DeviceData>(`/api/devices/${id}`)
  useTitle(data && `${data.Device.Name} · Settings`)
  if (!data) return <Loading error={error} />
  return (
    <>
      <DeviceHead device={data.Device} tab="settings" />
      {query.has('saved') && <Notice>Settings saved.</Notice>}
      <div className="grid two">
        <Display key={stamp} data={data} />
        <Actions data={data} />
      </div>
    </>
  )
}

function Display({ data }: { data: DeviceData }) {
  const d = data.Device
  const [brightness, setBrightness] = useState(d.Brightness)
  const { error, busy, submit } = useForm()
  const nudge = 'Shift the picture if the edge is hidden by a case or bezel'
  return (
    <section className="card">
      <h3>Display</h3>
      <ErrorNotice error={error} />
      <form onSubmit={submit(`/api/devices/${d.ID}`)} className="stack">
        <label>
          Name
          <input name="name" defaultValue={d.Name} maxLength={40} />
        </label>
        <label>
          Brightness <span className="muted small">{brightness}%</span>
          <input type="range" name="brightness" min={5} max={100} value={brightness} onChange={(e) => setBrightness(Number(e.target.value))} />
        </label>
        <div className="row">
          <label className="grow" title="The buttons follow the screen: the top button always moves up.">
            Screen direction
            <select name="flipped" defaultValue={d.Flipped ? '1' : '0'}>
              <option value="0">Normal</option>
              <option value="1">Upside down (180°)</option>
            </select>
          </label>
          <label className="shrink" title={nudge}>
            Nudge X (px)
            <input type="number" name="offset_x" defaultValue={d.OffsetX} min={-20} max={20} className="nudge" />
          </label>
          <label className="shrink" title={nudge}>
            Nudge Y (px)
            <input type="number" name="offset_y" defaultValue={d.OffsetY} min={-20} max={20} className="nudge" />
          </label>
        </div>
        <button className="btn" disabled={busy}>Save settings</button>
      </form>
    </section>
  )
}

type Result = { Msg: string; Error: boolean }

function Actions({ data }: { data: DeviceData }) {
  const d = data.Device
  const fw = data.FWVersion
  const [result, setResult] = useState<Result | null>(null)
  // Results fade out after a few seconds.
  useEffect(() => {
    if (!result) return
    const t = setTimeout(() => setResult(null), 4000)
    return () => clearTimeout(t)
  }, [result])

  async function act(action: string, fields: Record<string, string> = {}, confirm?: string) {
    if (confirm && !window.confirm(confirm)) return
    try {
      const r = await post<Result & { redirect?: string }>(`/api/devices/${d.ID}/do/${action}`, fields)
      if (r.redirect) navigate(r.redirect)
      else setResult(r)
    } catch (err) {
      setResult({ Msg: (err as Error).message, Error: true })
    }
  }
  function toast(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    act('toast', { text: String(new FormData(e.currentTarget).get('text') ?? '') })
  }

  return (
    <section className="card actions-card">
      <h3>Device actions</h3>
      <div>{result && <div className={result.Error ? 'action-msg err' : 'action-msg'}>{result.Msg}</div>}</div>

      <div className="action-group">
        <div className="label-like">Message</div>
        <form className="row" onSubmit={toast}>
          <input name="text" placeholder="Show a message on the screen…" maxLength={40} className="grow" />
          <button className="btn ghost">Send</button>
        </form>
      </div>

      <div className="action-group">
        <div className="label-like">Tools</div>
        <div className="row wrap">
          <button type="button" className="btn ghost small" onClick={() => act('identify')} title="Blinks the screen so you can tell which board this is">
            Blink screen
          </button>
          <button type="button" className="btn ghost small" onClick={() => act('sync')} title="Send the apps again">
            Re-send apps
          </button>
          <button type="button" className="btn ghost small" onClick={() => act('reboot')}>
            Restart
          </button>
        </div>
      </div>

      <div className="action-group">
        <div className="label-like">Firmware</div>
        <div className="row between fw-row">
          <span>
            {d.FWVersion} {d.NeedsUpdate ? <span className="badge off">update available</span> : <span className="badge ok">up to date</span>}
          </span>
          {d.NeedsUpdate ? (
            <button type="button" className="btn small" onClick={() => act('update', {}, `Install firmware ${fw} over WiFi? The device restarts when done.`)}>
              Update to {fw}
            </button>
          ) : (
            <button type="button" className="btn ghost small" onClick={() => act('update', {}, `Reinstall firmware ${fw} over WiFi?`)}>
              Reinstall
            </button>
          )}
        </div>
      </div>

      <div className="action-group danger-zone">
        <div className="row between">
          <span className="muted small">To change WiFi, open Settings on the board and hold D1 for 6 s.</span>
          <button
            type="button"
            className="btn danger small"
            onClick={() =>
              act(
                'forget',
                {},
                "Remove this device from your account? Its apps are uninstalled, and it shows a 6-digit code until someone links it again. You don't need this to update or reinstall the firmware.",
              )
            }
          >
            Remove device
          </button>
        </div>
      </div>
    </section>
  )
}
