import type { ActivityItem, CameraCard, ClimateSnapshot, DashboardSnapshot, HomeDevice, StatItem } from '../lib/types'

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

const mockDevices: HomeDevice[] = [
  {
    id: 'lock-front-door',
    name: 'Front Door',
    type: 'lock',
    zone: 'front_door',
    tone: 'healthy',
    detail: 'Deadbolt · Last locked 6:02 PM',
    lockState: 'locked',
  },
  {
    id: 'lock-back-door',
    name: 'Back Door',
    type: 'lock',
    zone: 'back_door',
    tone: 'warning',
    detail: 'Deadbolt · Last activity 3:15 PM',
    lockState: 'unlocked',
  },
  {
    id: 'lock-garage-side',
    name: 'Garage Side Entry',
    type: 'lock',
    zone: 'garage',
    tone: 'healthy',
    detail: 'Keypad lock · Last locked 8:00 AM',
    lockState: 'locked',
  },
  {
    id: 'door-garage',
    name: 'Garage Door',
    type: 'door',
    zone: 'garage',
    tone: 'warning',
    detail: 'Overhead · Opened 4:47 PM',
    openState: 'open',
  },
  {
    id: 'door-back',
    name: 'Back Door',
    type: 'door',
    zone: 'back_yard',
    tone: 'healthy',
    detail: 'Entry door · Closed 3:15 PM',
    openState: 'closed',
  },
  {
    id: 'window-living-room',
    name: 'Living Room',
    type: 'window',
    zone: 'living_room',
    tone: 'neutral',
    detail: 'Left panel · Opened 2:30 PM',
    openState: 'open',
  },
  {
    id: 'window-master',
    name: 'Master Bedroom',
    type: 'window',
    zone: 'master_bedroom',
    tone: 'healthy',
    detail: 'Both panels · Closed 9:00 AM',
    openState: 'closed',
  },
  {
    id: 'window-kitchen',
    name: 'Kitchen',
    type: 'window',
    zone: 'kitchen',
    tone: 'healthy',
    detail: 'Single panel · Closed 7:45 AM',
    openState: 'closed',
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

export const previewClimateSnapshot: ClimateSnapshot = {
  station_id: 'preview',
  generated_at: '',
  indoor: {
    sources: [],
    readings: [
      { name: 'temperature', display_name: 'Temperature', value: 0, unit: '°C', display_value: '--', domain: 'thermal', source: 'preview', quality: 'unavailable', recorded_at: '' },
      { name: 'humidity', display_name: 'Relative Humidity', value: 0, unit: '%', display_value: '--', domain: 'comfort', source: 'preview', quality: 'unavailable', recorded_at: '' },
      { name: 'co2', display_name: 'CO2', value: 0, unit: 'ppm', display_value: '--', domain: 'air_quality', source: 'preview', quality: 'unavailable', recorded_at: '' },
      { name: 'voc', display_name: 'VOCs', value: 0, unit: 'ppb', display_value: '--', domain: 'air_quality', source: 'preview', quality: 'unavailable', recorded_at: '' },
      { name: 'radon', display_name: 'Radon', value: 0, unit: 'Bq/m³', display_value: '--', domain: 'air_quality', source: 'preview', quality: 'unavailable', recorded_at: '' },
    ],
    stale: true,
  },
  outdoor: {
    sources: [],
    current: [
      { name: 'temperature', display_name: 'Temperature', value: 0, unit: '°C', display_value: '--', domain: 'thermal', source: 'preview', quality: 'unavailable', recorded_at: '' },
      { name: 'humidity', display_name: 'Relative Humidity', value: 0, unit: '%', display_value: '--', domain: 'comfort', source: 'preview', quality: 'unavailable', recorded_at: '' },
      { name: 'wind_speed', display_name: 'Wind Speed', value: 0, unit: 'm/s', display_value: '--', domain: 'weather', source: 'preview', quality: 'unavailable', recorded_at: '' },
      { name: 'precipitation', display_name: 'Precipitation', value: 0, unit: 'mm', display_value: '--', domain: 'weather', source: 'preview', quality: 'unavailable', recorded_at: '' },
    ],
    stale: true,
  },
}

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
  devices: mockDevices,
  activity: mockActivity,
  lastUpdatedLabel: 'preview',
}
