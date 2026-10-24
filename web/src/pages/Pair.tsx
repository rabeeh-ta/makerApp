import { useEffect, useState } from 'react'
import { ErrorNotice, Spinner } from '../components/common'
import { PairForm } from '../components/PairForm'
import { post } from '../lib/api'
import { useTitle } from '../lib/format'
import { Link, navigate, useInterval, useLocation } from '../lib/router'

export function Pair() {
  useTitle('Pair a device')
  const { query } = useLocation()
  const device = (query.get('device') ?? '').toUpperCase()
  const code = query.get('code') ?? ''
  return (
    <section className="narrow card">
      <h1>Pair a device</h1>
      {device && code ? <PairLink device={device} code={code} /> : <PairCode />}
    </section>
  )
}

// PairLink pairs from the link on the board's screen, retrying while the board connects.
function PairLink({ device, code }: { device: string; code: string }) {
  const [st, setSt] = useState<{ Waiting?: boolean; Error?: string }>({ Waiting: true })
  async function attempt() {
    try {
      const r = await post<{ redirect?: string; Waiting?: boolean; Error?: string }>('/api/pair/try', { device, code })
      if (r.redirect) navigate(r.redirect)
      else setSt(r)
    } catch (err) {
      setSt({ Error: (err as Error).message })
    }
  }
  useEffect(() => {
    attempt()
  }, [device, code])
  useInterval(attempt, st.Waiting ? 2000 : null)
  return (
    <>
      <p className="muted">
        Linking device <span className="mono">{device}</span> to your account.
      </p>
      {st.Waiting && (
        <p className="waiting">
          <Spinner /> Waiting for device <span className="mono">{device}</span> to reach the server...
        </p>
      )}
      <ErrorNotice error={st.Error ?? ''} />
    </>
  )
}

function PairCode() {
  return (
    <>
      <p className="muted">Enter the 6-digit code shown on the board.</p>
      <PairForm autoFocus />
      <p className="muted small">
        No code on the screen? <Link to="/flash">Flash the board</Link> first.
      </p>
    </>
  )
}
