import { useState } from 'react'
import { get } from '../lib/api'
import { ago, count, plural } from '../lib/format'
import { Link, useInterval } from '../lib/router'
import type { DeviceView } from '../lib/types'

export function Notice({ warn, children }: { warn?: boolean; children: React.ReactNode }) {
  return <div className={warn ? 'notice notice-warn' : 'notice'}>{children}</div>
}

export function ErrorNotice({ error }: { error: string }) {
  return error ? <Notice warn>{error}</Notice> : null
}

export function Loading({ error }: { error?: string }) {
  if (error) return <Notice warn>{error}</Notice>
  return (
    <p className="waiting">
      <span className="spinner" /> Loading…
    </p>
  )
}

export function Spinner() {
  return <span className="spinner" />
}

// Byline is an app's publisher with its install count.
export function Byline({ by, installs }: { by: string; installs: number }) {
  return (
    <span className="byline">
      <span className="publisher">@{by}</span>
      <span className="installs" title={plural(installs, 'install')}>
        <svg viewBox="0 0 16 16" aria-hidden="true">
          <path d="M8 2.5v7.5M4.5 6.5 8 10l3.5-3.5M3 13.5h10" />
        </svg>
        {count(installs)}
        <span className="sr-only"> installs</span>
      </span>
    </span>
  )
}

export function OnlineBadge({ online }: { online: boolean }) {
  return <span className={online ? 'badge ok' : 'badge off'}>{online ? 'Online' : 'Offline'}</span>
}

// DeviceBadge shows whether the board is connected, checking every 5 seconds.
function DeviceBadge({ device }: { device: DeviceView }) {
  const [st, setSt] = useState({ Online: device.Online, LastSeen: device.LastSeen })
  useInterval(() => {
    get<typeof st>(`/api/devices/${device.ID}/status`).then(setSt, () => {})
  }, 5000)
  return <span className={st.Online ? 'badge ok' : 'badge off'}>{st.Online ? 'Online' : `Offline · seen ${ago(st.LastSeen)}`}</span>
}

// MyAppsHead is the title and tabs of My apps: the apps you made, and the
// data sources that feed them.
export function MyAppsHead({ tab, intro, actions }: { tab: 'apps' | 'sources'; intro: string; actions: React.ReactNode }) {
  return (
    <>
      <div className="page-head">
        <div>
          <h1>My apps</h1>
          <p className="muted small">{intro}</p>
        </div>
        <div className="row">{actions}</div>
      </div>
      <nav className="tabs-nav">
        <Link to="/apps" className={tab === 'apps' ? 'on' : undefined}>
          Apps
        </Link>
        <Link to="/sources" className={tab === 'sources' ? 'on' : undefined}>
          Data sources
        </Link>
      </nav>
    </>
  )
}

// DeviceHead is the title and tabs of a device's pages.
export function DeviceHead({ device, tab, paired }: { device: DeviceView; tab: 'screensaver' | 'apps' | 'settings'; paired?: boolean }) {
  return (
    <>
      <div className="page-head device-head">
        <div>
          <p className="crumbs">
            <Link to="/devices">Devices</Link> /
          </p>
          <h1>{device.Name}</h1>
          <p className="muted small">
            <span className="mono">{device.ID}</span> · firmware {device.FWVersion}
            {device.NeedsUpdate && ' · new firmware installs by itself'}
          </p>
        </div>
        <DeviceBadge key={device.ID} device={device} />
      </div>
      <nav className="tabs-nav">
        <Link to={`/devices/${device.ID}/screensaver`} className={tab === 'screensaver' ? 'on' : undefined}>
          Screensaver
        </Link>
        <Link to={`/devices/${device.ID}`} className={tab === 'apps' ? 'on' : undefined}>
          Apps <span className="count">{device.AppCount}</span>
        </Link>
        <Link to={`/devices/${device.ID}/settings`} className={tab === 'settings' ? 'on' : undefined}>
          Settings
        </Link>
      </nav>
      {paired && <Notice>Paired. Install an app to get started.</Notice>}
    </>
  )
}
