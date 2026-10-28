import { Byline, Loading } from '../components/common'
import { Thumb } from '../components/DeviceScreen'
import { useTitle } from '../lib/format'
import { Link, useData } from '../lib/router'
import type { Device, Screen } from '../lib/types'

type Listing = { ID: string; Name: string; Desc: string; By: string; Installs: number; Preview: Screen | null; On: Device[] | null }

// Store is the App Store: makerApp's own apps and the ones people publish,
// most installed first. Each opens its own page to install and set it up.
export function Store() {
  useTitle('App Store')
  const { data, error } = useData<{ Apps: Listing[] | null }>('/api/store')
  if (!data) return <Loading error={error} />
  return (
    <>
      <div className="page-head">
        <div>
          <h1>App Store</h1>
          <p className="muted small">
            Apps by makerInc and by people who published theirs, most installed first. In apps that use the middle button, hold it for a second to leave.
          </p>
        </div>
      </div>
      <div className="grid cards">
        {(data.Apps ?? []).map((l) => (
          <Link className="card screen-card screen-link" to={`/store/${l.ID}`} key={l.ID}>
            <Thumb s={l.Preview} empty="No preview" />
            <div className="row between">
              <strong>{l.Name}</strong>
              <Byline by={l.By} installs={l.Installs} />
            </div>
            <div className="muted small">{l.Desc}</div>
            {l.On?.length ? <span className="pill ok">On {l.On.map((d) => d.Name).join(', ')}</span> : null}
          </Link>
        ))}
      </div>
    </>
  )
}
