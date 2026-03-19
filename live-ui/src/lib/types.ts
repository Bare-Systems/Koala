export type Tone = 'healthy' | 'warning' | 'critical' | 'neutral'

export type LiveTab = 'home' | 'activity' | 'cameras' | 'climate' | 'profile'

export type KoalaSettings = {
  baseUrl: string
  token: string
  polarBaseUrl: string
  polarToken: string
  viewerName: string
  notificationsEnabled: boolean
}

export type DeviceType = 'camera' | 'lock' | 'door' | 'window'

export type LockState = 'locked' | 'unlocked' | 'unknown'

export type OpenState = 'open' | 'closed' | 'unknown'

export type HomeDevice = {
  id: string
  name: string
  type: DeviceType
  zone: string
  tone: Tone
  detail: string
  lockState?: LockState
  openState?: OpenState
  statusLabel?: string
  snapshotUrl?: string
}

export type CameraCard = {
  id: string
  name: string
  zoneLabel: string
  statusLabel: string
  detail: string
  tone: Tone
  snapshotUrl?: string
}

export type ActivityItem = {
  id: string
  title: string
  body: string
  timeLabel: string
  tone: Tone
  saveKey: string
}

export type StatItem = {
  label: string
  value: string
  detail: string
  tone: Tone
}

export type PolarQualityFlag = 'good' | 'estimated' | 'outlier' | 'unavailable'

export type PolarClimateMetric = {
  name: string
  display_name: string
  value: number
  unit: string
  display_value: string
  domain: string // "air_quality" | "thermal" | "comfort" | "weather" | "other"
  source: string
  quality: PolarQualityFlag
  recorded_at: string
}

export type PolarIndoorClimate = {
  sources: string[]
  readings: PolarClimateMetric[]
  last_reading_at?: string
  stale: boolean
}

export type PolarOutdoorClimate = {
  sources: string[]
  current: PolarClimateMetric[]
  forecast?: Array<{
    time: string
    temperature_c: number
    humidity_pct: number
    wind_speed_ms: number
    precip_mm: number
  }>
  last_fetched_at?: string
  fresh_until?: string
  stale: boolean
}

export type ClimateSnapshot = {
  station_id: string
  generated_at: string
  indoor: PolarIndoorClimate
  outdoor: PolarOutdoorClimate
}

export type DashboardSnapshot = {
  headline: string
  subheadline: string
  packageSummary: string
  zoneSummary: string
  serviceLabel: string
  serviceTone: Tone
  stats: StatItem[]
  cameras: CameraCard[]
  devices: HomeDevice[]
  activity: ActivityItem[]
  lastUpdatedLabel: string
}
