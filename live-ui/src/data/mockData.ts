import type { ActivityItem, CameraCard, DashboardSnapshot, StatItem } from '../lib/types'

export const tabPrompts = [
  'Refresh live home status',
  'Check for a package at the door',
  'Review recent Koala alerts',
]

const mockStats: StatItem[] = [
  {
    label: 'System',
    value: 'Preview',
    detail: 'Waiting for a live Koala endpoint',
    tone: 'neutral',
  },
  {
    label: 'Package',
    value: 'Unknown',
    detail: 'Run a package check when connected',
    tone: 'warning',
  },
  {
    label: 'Cameras',
    value: '0 online',
    detail: 'Live camera roster will appear here',
    tone: 'neutral',
  },
]

const mockCameras: CameraCard[] = [
  {
    id: 'preview-front',
    name: 'Front Door',
    zoneLabel: 'front_door',
    statusLabel: 'preview',
    detail: 'Connect Koala to load the real camera roster.',
    tone: 'neutral',
  },
  {
    id: 'preview-drive',
    name: 'Driveway',
    zoneLabel: 'driveway',
    statusLabel: 'preview',
    detail: 'Consumer playback lives here once the media path is ready.',
    tone: 'neutral',
  },
]

const mockActivity: ActivityItem[] = [
  {
    id: 'preview-1',
    title: 'Koala Live is ready for connection',
    body: 'This consumer UI is live. Point it at a Koala service to replace the preview feed.',
    timeLabel: 'now',
    tone: 'healthy',
    saveKey: 'preview-1',
  },
  {
    id: 'preview-2',
    title: 'Saved moments are local for now',
    body: 'Until recording APIs exist, saved moments are stored in the browser only.',
    timeLabel: 'preview',
    tone: 'warning',
    saveKey: 'preview-2',
  },
]

export const previewSnapshot: DashboardSnapshot = {
  headline: 'Home status ready',
  subheadline:
    'Koala Live is the consumer-facing home monitor. Connect a live Koala endpoint to replace this preview data.',
  packageSummary: 'Package state unavailable in preview mode.',
  zoneSummary: 'Front door zone not yet connected.',
  serviceLabel: 'preview',
  serviceTone: 'neutral',
  stats: mockStats,
  cameras: mockCameras,
  activity: mockActivity,
  lastUpdatedLabel: 'preview',
}
