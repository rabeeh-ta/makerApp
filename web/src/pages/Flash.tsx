import { useEffect, useState } from 'react'
import { Loading, Notice, Spinner } from '../components/common'
import { get, post } from '../lib/api'
import { useTitle } from '../lib/format'
import { Link, useData, useInterval, useLocation } from '../lib/router'

type Board = { ID: string; Name: string; Maker: string; Specs: string }
type Info = { Boards: Board[]; ServerURL: string; Insecure: boolean; FWVersion: string; Available: boolean }

declare module 'react' {
  namespace JSX {
    interface IntrinsicElements {
      'esp-web-install-button': React.DetailedHTMLProps<React.HTMLAttributes<HTMLElement>, HTMLElement> & { manifest?: string }
    }
  }
}

export function Flash() {
  useTitle('Add a device')
  const { data, error } = useData<Info>('/api/flash')
  const boardID = useLocation().query.get('board')
  if (!data) return <Loading error={error} />
  const board = data.Boards.find((b) => b.ID === boardID)
  return board ? <Install key={board.ID} board={board} info={data} /> : <Picker boards={data.Boards} />
}

function Picker({ boards }: { boards: Board[] }) {
  return (
    <>
      <div className="page-head">
        <div>
          <h1>Add a device</h1>
          <p className="muted">Which board are you setting up?</p>
        </div>
      </div>
      <div className="grid cards">
        {boards.map((b) => (
          <Link className="card board-card" to={`/flash?board=${b.ID}`} key={b.ID}>
            <div className="board-thumb">
              <svg viewBox="0 0 240 120" role="img" aria-label={`${b.Name} illustration`}>
                <rect x="18" y="14" width="204" height="92" rx="8" fill="#1d1d1b" stroke="#2c2c29" />
                <rect x="8" y="48" width="12" height="24" rx="3" fill="#3a3a37" />
                <rect x="34" y="24" width="136" height="72" rx="3" fill="#000" stroke="#2c2c29" />
                <rect x="42" y="32" width="38" height="5" rx="1" fill="#F54E00" />
                <rect x="42" y="48" width="70" height="16" rx="2" fill="#f0eee8" />
                <rect x="42" y="74" width="54" height="5" rx="1" fill="#5f5d58" />
                <g fill="#3a3a37" stroke="#4a4a46">
                  <circle cx="194" cy="38" r="6" />
                  <circle cx="194" cy="60" r="6" />
                  <circle cx="194" cy="82" r="6" />
                </g>
                <g fill="#8a877f" fontFamily="JetBrains Mono, monospace" fontSize="7">
                  <text x="205" y="40.5">D0</text>
                  <text x="205" y="62.5">D1</text>
                  <text x="205" y="84.5">D2</text>
                </g>
              </svg>
            </div>
            <strong>{b.Name}</strong>
            <span className="muted small">
              {b.Maker} · {b.Specs}
            </span>
          </Link>
        ))}
      </div>
    </>
  )
}

// loadInstaller adds ESP Web Tools' <esp-web-install-button> once.
function loadInstaller() {
  if (document.getElementById('ewt')) return
  const s = document.createElement('script')
  s.id = 'ewt'
  s.type = 'module'
  s.src = '/static/ewt/install-button.js'
  document.head.appendChild(s)
}

function Install({ board, info }: { board: Board; info: Info }) {
  useTitle(board.Name)
  const [claim, setClaim] = useState<{ Claim: string; ManifestURL: string } | null>(null)
  const [error, setError] = useState('')
  useEffect(() => {
    loadInstaller()
    post<{ Claim: string; ManifestURL: string }>('/api/flash/claims').then(setClaim, (e: Error) => setError(e.message))
  }, [])
  return (
    <>
      <div className="page-head">
        <div>
          <p className="crumbs">
            <Link to="/flash">Add a device</Link> /
          </p>
          <h1>{board.Name}</h1>
        </div>
      </div>
      {!info.Available && (
        <Notice warn>
          This server was built without firmware. Run <code>make firmware</code> to include it.
        </Notice>
      )}
      {info.Insecure && <Notice warn>Browsers only allow USB flashing over HTTPS or on localhost. Open this page over https:// to flash.</Notice>}
      {error && <Notice warn>{error}</Notice>}

      <div className="grid two wide-left">
        <div className="stack">
          <section className="card flow-step">
            <span className="step">1</span>
            <div>
              <h3>Put the board in install mode</h3>
              <p className="muted small">First time only.</p>
              <ol className="mini">
                <li>
                  Hold <b>D0</b>
                </li>
                <li>
                  Press <b>RESET</b>
                </li>
                <li>Release D0 (screen goes dark)</li>
              </ol>
            </div>
          </section>

          <section className="card flow-step">
            <span className="step">2</span>
            <div>
              <h3>Install the makerDevice firmware</h3>
              <p className="muted">
                In the list, pick <b>USB JTAG/serial debug unit</b> → Connect → Install.
              </p>
              <div className="install">
                {claim && (
                  <esp-web-install-button manifest={claim.ManifestURL}>
                    <button slot="activate" className="btn btn-lg">
                      Install makerDevice {info.FWVersion}
                    </button>
                    <span slot="unsupported" className="notice notice-warn">
                      This browser can't talk to USB devices. Open this page in Chrome or Edge on a computer.
                    </span>
                    <span slot="not-allowed" className="notice notice-warn">
                      USB flashing needs HTTPS (or localhost).
                    </span>
                  </esp-web-install-button>
                )}
              </div>
              <Notice>
                <div>
                  <b>After “Installation complete”, press RESET once.</b>
                </div>
              </Notice>
              <p className="muted small">Not listed? Try a data cable and repeat step 1.</p>
            </div>
          </section>

          <section className="card flow-step">
            <span className="step">3</span>
            <div>
              <h3>Connect it to WiFi</h3>
              <Notice warn>
                <div>
                  <b>2.4 GHz WiFi only.</b> iPhone hotspot: turn on Maximize Compatibility.
                </div>
              </Notice>
              <p className="muted">Enter WiFi in the same window. If it closes first, use your phone:</p>
              <ol className="mini">
                <li>
                  Join <b>makerDevice-XXXX</b>
                </li>
                <li>
                  Open <b className="mono">192.168.4.1</b>
                </li>
                <li>Pick your WiFi → Save</li>
              </ol>
            </div>
          </section>

          <section className="card flow-step">
            <span className="step">4</span>
            <div>
              <h3>It shows up here by itself</h3>
              <p className="muted">No code needed. Watch the status.</p>
            </div>
          </section>
        </div>

        <aside className="stack sticky">
          <section className="card">
            <h3>Status</h3>
            {claim && <FlashStatus claim={claim.Claim} />}
            <p className="muted small">
              Boards installed from this page connect to
              <br />
              <span className="mono">{info.ServerURL}</span>
            </p>
          </section>
          <section className="card">
            <h3>Board says…</h3>
            <dl className="screen-help">
              <dt>WiFi setup</dt>
              <dd>Do step 3</dd>
              <dt>Not found</dt>
              <dd>Use 2.4 GHz</dd>
              <dt>Wrong password?</dt>
              <dd>Redo WiFi</dd>
              <dt>Can't reach server</dt>
              <dd>Check the internet</dd>
              <dt>6-digit code</dt>
              <dd>
                <Link to="/pair">Enter it</Link>
              </dd>
            </dl>
          </section>
        </aside>
      </div>
    </>
  )
}

function FlashStatus({ claim }: { claim: string }) {
  const [device, setDevice] = useState<{ ID: string; Name: string } | null>(null)
  useInterval(
    () => {
      get<{ Device: { ID: string; Name: string } | null }>(`/api/flash/status?claim=${claim}`).then((r) => setDevice(r.Device), () => {})
    },
    device ? null : 2000,
  )
  if (!device) {
    return (
      <div className="waiting">
        <Spinner /> Waiting for the device to come online. This page updates by itself.
      </div>
    )
  }
  return (
    <Notice>
      <strong>{device.Name} is connected.</strong>{' '}
      <Link className="btn" to={`/devices/${device.ID}`}>Open the device</Link>{' '}
      <Link className="btn ghost" to="/sources/new">Add a data source</Link>
    </Notice>
  )
}
