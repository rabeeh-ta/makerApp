import { useEffect } from 'react'
import { useTitle } from '../lib/format'
import { Link, navigate } from '../lib/router'
import type { Me } from '../lib/types'

export function Landing({ me }: { me: Me }) {
  useTitle('Your desk display, your data')
  useEffect(() => {
    if (me.User) navigate('/devices', { replace: true })
  }, [me.User])
  if (me.User) return null
  return (
    <>
      <section className="hero">
        <div>
          <p className="eyebrow">makerApp</p>
          <h1>Put any API on the little screen on your desk.</h1>
          <p className="lead">
            Flash your ESP32-S3 board from the browser, connect it to WiFi, then point it at any API. Choose the fields to show, decide what
            the three buttons do, and press save. The device updates within seconds.
          </p>
          <p className="row">
            {me.AllowSignup && <Link className="btn" to="/signup">Create an account</Link>}
            <Link className="btn ghost" to="/login">Log in</Link>
          </p>
        </div>
        <div className="hero-device">
          <div className="board">
            <div className="lcd lcd-big" style={{ '--accent': '#F54E00' } as React.CSSProperties}>
              <div className="lcd-title">Signups today</div>
              <div className="lcd-value">1,284</div>
              <div className="lcd-sub" style={{ color: '#2BB673' }}>+12% vs yesterday</div>
            </div>
            <div className="board-buttons">
              <i />
              <i />
              <i />
            </div>
          </div>
        </div>
      </section>
      <section className="grid three steps">
        <div className="card">
          <span className="step">1</span>
          <h3>Flash in the browser</h3>
          <p className="muted">Plug in over USB, click Install, enter WiFi.</p>
        </div>
        <div className="card">
          <span className="step">2</span>
          <h3>Connect any data</h3>
          <p className="muted">Any API, a webhook, or fixed data.</p>
        </div>
        <div className="card">
          <span className="step">3</span>
          <h3>Build apps</h3>
          <p className="muted">Group screens into apps and install them. The board lists them in its Apps drawer.</p>
        </div>
      </section>
    </>
  )
}
