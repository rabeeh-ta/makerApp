import { Byline, DeviceHead, ErrorNotice, Loading, Notice } from '../components/common'
import { Thumb } from '../components/DeviceScreen'
import { plural, useTitle } from '../lib/format'
import { Link, PostButton, useData, useForm, useLocation } from '../lib/router'
import type { DeviceView, Screen } from '../lib/types'

export type DeviceData = {
  Device: DeviceView
  FWVersion: string
  DataKB: string
  DataMaxKB: number
  DataLevel: '' | 'near' | 'full'
  Installed: { id: string; name: string; screens: Screen[]; Catalog: boolean; By: string; Preview: Screen | null; First: boolean; Last: boolean; NeedsFW: string }[] | null
  Choices: { ID: string; Name: string; Desc: string; By: string; Installs: number; ScreenCount: number }[] | null
}

export function Device({ id }: { id: string }) {
  const { query } = useLocation()
  const { data, error } = useData<DeviceData>(`/api/devices/${id}`)
  const newApp = useForm()
  useTitle(data?.Device.Name)
  if (!data) return <Loading error={error} />
  const dev = data.Device
  const installed = data.Installed ?? []
  const choices = data.Choices ?? []
  return (
    <>
      <DeviceHead device={dev} tab="apps" paired={query.has('paired')} />
      <div className="row between section-head">
        <span className="muted small">
          The board lists these in its Apps drawer, in this order. The middle button opens and closes an app; the top and bottom buttons move.
        </span>
        <form onSubmit={newApp.submit('/api/apps')} className="row">
          <input type="hidden" name="device" value={dev.ID} />
          <input name="name" placeholder="New app name" maxLength={40} required className="inline-input" />
          <button className="btn small" disabled={newApp.busy}>New app</button>
        </form>
      </div>
      <ErrorNotice error={newApp.error} />
      {data.DataLevel === 'full' && (
        <Notice warn>
          The apps' content is {data.DataKB} KB, more than the {data.DataMaxKB} KB the board can receive at once, so it can't update. Uninstall an
          app or show fewer list rows.
        </Notice>
      )}
      {data.DataLevel === 'near' && (
        <Notice warn>
          The apps' content is {data.DataKB} of the {data.DataMaxKB} KB the board can receive at once. Adding more may stop it from updating.
        </Notice>
      )}
      {installed.length ? (
        <div className="grid cards">
          {installed.map((a) => (
            <div className="card screen-card" key={a.id}>
              <Link className="screen-link" to={a.Catalog ? '/apps' : `/apps/${a.id}`}>
                <Thumb s={a.Preview} empty="No screens yet" />
                <div className="row between">
                  <strong>{a.name}</strong>
                  {a.Catalog ? <span className="publisher">@{a.By}</span> : <span className="muted small">{plural(a.screens.length, 'screen')}</span>}
                </div>
                {a.NeedsFW && <span className="warn small">Shows up after a firmware update to {a.NeedsFW} (Settings tab).</span>}
              </Link>
              <div className="card-actions">
                <PostButton action={`/api/devices/${dev.ID}/apps/${a.id}/move`} data={{ dir: 'up' }} className="btn ghost small icon" disabled={a.First} title="Move earlier">
                  ↑
                </PostButton>
                <PostButton action={`/api/devices/${dev.ID}/apps/${a.id}/move`} data={{ dir: 'down' }} className="btn ghost small icon" disabled={a.Last} title="Move later">
                  ↓
                </PostButton>
                <span className="grow" />
                {!a.Catalog && <Link className="btn ghost small" to={`/apps/${a.id}`}>Edit</Link>}
                <PostButton action={`/api/apps/${a.id}/uninstall`} data={{ device: dev.ID }} className="btn ghost small">
                  Uninstall
                </PostButton>
              </div>
            </div>
          ))}
        </div>
      ) : (
        <div className="card empty">
          <h3>No apps installed</h3>
          <p className="muted">The board's Apps drawer is empty. Install an app below, or make a new one.</p>
        </div>
      )}

      <p className="muted small data-size">
        Apps data sent to the board: {data.DataKB} of {data.DataMaxKB} KB
      </p>
      {choices.length > 0 && (
        <>
          <h3 className="section-title">Install an app</h3>
          <div className="card install-list">
            {choices.map((c) => (
              <div className="install-row" key={c.ID}>
                <div className="grow">
                  <strong>{c.Name}</strong>
                  {c.By && (
                    <>
                      {' '}
                      <Byline by={c.By} installs={c.Installs} />
                    </>
                  )}
                  <div className="muted small">{c.By ? c.Desc : `Yours · ${plural(c.ScreenCount, 'screen')}`}</div>
                </div>
                <PostButton action={`/api/apps/${c.ID}/install`} data={{ device: dev.ID }}>
                  Install
                </PostButton>
              </div>
            ))}
          </div>
        </>
      )}
    </>
  )
}
