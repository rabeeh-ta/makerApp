import { Loading, OnlineBadge } from '../components/common'
import { Thumb } from '../components/DeviceScreen'
import { plural, useTitle } from '../lib/format'
import { Link, useData, useInterval } from '../lib/router'
import type { DeviceView } from '../lib/types'

type Data = {
  Devices: DeviceView[] | null
  HasDevice: boolean
  HasSource: boolean
  HasScreen: boolean
  Installed: boolean
  FirstDevice: string
}

export function Devices() {
  useTitle('Devices')
  const { data, error, reload } = useData<Data>('/api/devices')
  useInterval(reload, 5000)
  if (!data) return <Loading error={error} />
  const d = data
  const step = (done: boolean, label: string, button: React.ReactNode) => (
    <li className={done ? 'done' : undefined}>
      <span className="tick" />
      <div>
        <strong>{label}</strong>
      </div>
      {!done && button}
    </li>
  )
  return (
    <>
      <div className="page-head">
        <div>
          <h1>Devices</h1>
        </div>
        {d.HasDevice && (
          <div className="row">
            <Link className="btn ghost" to="/pair">Enter a code</Link>
            <Link className="btn" to="/flash">Add a device</Link>
          </div>
        )}
      </div>

      {!(d.HasDevice && d.HasSource && d.HasScreen && d.Installed) && (
        <section className="card onboarding">
          <div>
            <h3>Get started</h3>
          </div>
          <ol className="checklist">
            {step(
              d.HasDevice,
              'Add your board',
              // A span: checklist divs are columns.
              <span className="row">
                <Link className="btn small ghost" to="/pair">Enter a code</Link>
                <Link className="btn small" to="/flash">Add device</Link>
              </span>,
            )}
            {step(d.HasSource, 'Connect some data', <Link className={d.HasDevice ? 'btn small' : 'btn small ghost'} to="/sources/new">Add data</Link>)}
            {step(d.HasScreen, 'Build an app', <Link className={d.HasSource ? 'btn small' : 'btn small ghost'} to="/apps">New app</Link>)}
            {step(
              d.Installed,
              'Install it on your board',
              <Link className={d.HasScreen ? 'btn small' : 'btn small ghost'} to={d.FirstDevice ? `/devices/${d.FirstDevice}` : '/flash'}>
                Install
              </Link>,
            )}
          </ol>
        </section>
      )}

      {d.HasDevice && (
        <div className="grid cards">
          {(d.Devices ?? []).map((v) => (
            <Link className="card device-card" to={`/devices/${v.ID}`} key={v.ID}>
              <Thumb s={v.Preview} empty="No apps yet" />
              <div className="row between">
                <strong>{v.Name}</strong>
                <OnlineBadge online={v.Online} />
              </div>
              <div className="muted small">
                {plural(v.AppCount, 'app')} · firmware {v.FWVersion}
                {v.NeedsUpdate && ' · updating'}
              </div>
            </Link>
          ))}
          <Link className="card add-card" to="/flash">
            <span className="plus">+</span>
            <strong>Add a device</strong>
          </Link>
        </div>
      )}
    </>
  )
}
