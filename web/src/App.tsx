import { useCallback, useEffect, useState } from 'react'
import { Layout } from './components/Layout'
import { Loading } from './components/common'
import { get, whenSignedOut } from './lib/api'
import { useTitle } from './lib/format'
import { Link, navigate, useLocation } from './lib/router'
import type { Me } from './lib/types'
import { Account } from './pages/Account'
import { AppPage } from './pages/App'
import { Apps } from './pages/Apps'
import { Auth } from './pages/Auth'
import { Connect } from './pages/Connect'
import { Device } from './pages/Device'
import { DeviceScreensaver } from './pages/DeviceScreensaver'
import { DeviceSettings } from './pages/DeviceSettings'
import { Devices } from './pages/Devices'
import { Flash } from './pages/Flash'
import { Landing } from './pages/Landing'
import { OAuthAuthorize } from './pages/OAuthAuthorize'
import { Pair } from './pages/Pair'
import { Publish } from './pages/Publish'
import { ScreenEdit } from './pages/ScreenEdit'
import { SourceEdit } from './pages/SourceEdit'
import { Sources } from './pages/Sources'
import { Store } from './pages/Store'
import { StoreApp } from './pages/StoreApp'

type Route = { re: RegExp; page: (p: string[], me: Me, refreshMe: () => Promise<void>) => React.ReactNode; open?: boolean }

const routes: Route[] = [
  { re: /^\/$/, page: (_, me) => <Landing me={me} />, open: true },
  { re: /^\/(login|signup)$/, page: ([mode], me, refreshMe) => <Auth key={mode} mode={mode as 'login' | 'signup'} me={me} refreshMe={refreshMe} />, open: true },
  { re: /^\/flash$/, page: () => <Flash /> },
  { re: /^\/pair$/, page: () => <Pair /> },
  { re: /^\/devices$/, page: () => <Devices /> },
  { re: /^\/devices\/([^/]+)$/, page: ([id]) => <Device id={id} /> },
  { re: /^\/devices\/([^/]+)\/settings$/, page: ([id]) => <DeviceSettings id={id} /> },
  { re: /^\/devices\/([^/]+)\/screensaver$/, page: ([id]) => <DeviceScreensaver key={id} id={id} /> },
  { re: /^\/sources$/, page: () => <Sources /> },
  { re: /^\/sources\/([^/]+)$/, page: ([id]) => <SourceEdit key={id} id={id === 'new' ? '' : id} /> },
  { re: /^\/apps$/, page: (_, me) => <Apps me={me} /> },
  { re: /^\/apps\/([^/]+)$/, page: ([id]) => <AppPage id={id} /> },
  { re: /^\/apps\/([^/]+)\/publish$/, page: ([id]) => <Publish key={id} id={id} /> },
  { re: /^\/screens\/([^/]+)$/, page: ([id]) => <ScreenEdit key={id} id={id === 'new' ? '' : id} /> },
  { re: /^\/store$/, page: () => <Store /> },
  { re: /^\/store\/([^/]+)$/, page: ([id]) => <StoreApp key={id} id={id} /> },
  { re: /^\/connect$/, page: () => <Connect /> },
  { re: /^\/account$/, page: () => <Account /> },
  { re: /^\/oauth\/authorize$/, page: () => <OAuthAuthorize /> },
]

export function App() {
  const { path } = useLocation()
  const [me, setMe] = useState<Me | null>(null)
  const refreshMe = useCallback(() => get<Me>('/api/me').then(setMe), [])
  useEffect(() => {
    refreshMe()
    whenSignedOut(() => {
      setMe((m) => (m ? { ...m, User: null } : m))
    })
  }, [refreshMe])

  let match: RegExpMatchArray | null = null
  const route = routes.find((r) => (match = path.match(r.re)))
  const needsLogin = me && !me.User && route && !route.open
  useEffect(() => {
    if (needsLogin) navigate('/login?next=' + encodeURIComponent(location.pathname + location.search), { replace: true })
  }, [needsLogin])

  if (!me) return <Layout me={{ User: null, AllowSignup: false }} path={path} onSignOut={() => {}}><Loading /></Layout>
  return (
    <Layout me={me} path={path} onSignOut={() => setMe({ ...me, User: null })}>
      {needsLogin ? null : route && match ? route.page([...(match as RegExpMatchArray)].slice(1).map(decodeURIComponent), me, refreshMe) : <NotFound />}
    </Layout>
  )
}

function NotFound() {
  useTitle('Not found')
  return (
    <section className="narrow card">
      <h1>Page not found</h1>
      <p className="muted">
        <Link to="/">Go to the start page</Link>
      </p>
    </section>
  )
}
