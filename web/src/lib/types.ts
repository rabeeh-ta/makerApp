// Shapes of the API's JSON. Store records use Go's field names; screens and
// presets use their JSON tags. Go sends empty lists as null.

export type Screen = {
  id: string
  layout: string
  title: string
  accent?: string
  bg?: string
  value?: string
  sub?: string
  subColor?: string
  body?: string
  items?: [string, string][]
  series?: number[]
  pct?: number
  name?: string
  error?: string
}

export type Me = { User: { Email: string; Username: string } | null; AllowSignup: boolean }

export type Device = {
  ID: string
  UserID: string
  Name: string
  FWVersion: string
  Board: string
  IP: string
  RSSI: number
  Brightness: number
  LastSeen: number
  CreatedAt: number
  Flipped: boolean
  OffsetX: number
  OffsetY: number
}

export type DeviceView = Device & { Online: boolean; AppCount: number; NeedsUpdate: boolean; Preview: Screen | null }

export type Source = {
  ID: string
  UserID: string
  Name: string
  Kind: 'http' | 'static' | 'push'
  Method: string
  URL: string
  Body: string
  IntervalSec: number
  PushToken: string
  LastData: string
  LastStatus: number
  LastError: string
  LastFetchedAt: number
  CreatedAt: number
  PerUser: boolean // personal: everyone who installs an app using it sends their own data
  Example: string // personal: JSON previews show people who haven't sent their own
}

export type App = {
  ID: string
  UserID: string
  Name: string
  CycleSec: number
  UpdatedAt: number
  CreatedAt: number
  Description: string
  PublishedAt: number
  MadeBy: string // the AI app that made it, "" if made here
  Setup: string // its README, in Markdown: what it shows and how to set it up, on its App Store page
}

export type StoredScreen = {
  ID: string
  UserID: string
  Name: string
  SourceID: string
  Layout: string
  Spec: string
  UpdatedAt: number
  CreatedAt: number
  AppID: string
}

export type Spec = {
  title: string
  accent: string
  bg: string
  value: string
  sub: string
  sub_color: string
  body: string
  items_path: string
  item_label: string
  item_value: string
  max_items: number
  series_path: string
  pct_value: string
  pct_max: string
}

// SourceData is a source's latest response, as the source page shows it.
export type SourceData = { Source: Source; Pretty: string; Paths: unknown }
