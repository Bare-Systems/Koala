export type Tone = 'healthy' | 'warning' | 'critical' | 'neutral'

export type LiveTab = 'home' | 'activity' | 'cameras' | 'profile'

export type KoalaSettings = {
  baseUrl: string
  token: string
  viewerName: string
  notificationsEnabled: boolean
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

export type DashboardSnapshot = {
  headline: string
  subheadline: string
  packageSummary: string
  zoneSummary: string
  serviceLabel: string
  serviceTone: Tone
  stats: StatItem[]
  cameras: CameraCard[]
  activity: ActivityItem[]
  lastUpdatedLabel: string
}
