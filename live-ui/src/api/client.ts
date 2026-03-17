import { previewSnapshot } from '../data/mockData'
import type {
  ActivityItem,
  CameraCard,
  DashboardSnapshot,
  KoalaSettings,
  StatItem,
  Tone,
} from '../lib/types'

type ToolEnvelope<T> = {
  status: string
  explanation?: string
  next_action?: string
  data: T
}

type KoalaHealth = {
  status: string
  ingest: string
  inference: string
  mcp: string
  uptime_seconds: number
}

type KoalaCamera = {
  id: string
  name: string
  zone_id: string
  front_door: boolean
  status: string
}

type KoalaZoneState = {
  zone_id: string
  observed_at: string
  stale: boolean
  entities: Array<{ label?: string; confidence?: number }>
}

type KoalaPackageState = {
  package_present: boolean
  confidence: number
  observed_at: string
  stale: boolean
}

type KoalaIncident = {
  camera_id: string
  type: string
  severity: string
  message: string
  occurred_at: string
}

type KoalaIngestStatus = {
  cameras: Record<
    string,
    {
      last_status: string
      consecutive_failures: number
      last_error?: string
      last_capture_at?: string
    }
  >
  incidents: KoalaIncident[]
}

async function requestJson<T>(input: string, init: RequestInit): Promise<T> {
  const response = await fetch(input, init)
  if (!response.ok) {
    const detail = await response.text()
    throw new Error(detail || `Request failed with status ${response.status}`)
  }
  return (await response.json()) as T
}

function normalizeBaseUrl(value: string): string {
  return value.trim().replace(/\/+$/, '')
}

function canRequest(value: string): boolean {
  return /^https?:\/\//.test(value.trim())
}

function buildHeaders(token: string, json = true): HeadersInit {
  const headers: HeadersInit = {}
  if (json) {
    headers['Content-Type'] = 'application/json'
  }
  if (token.trim()) {
    headers.Authorization = `Bearer ${token.trim()}`
  }
  return headers
}

function formatIsoLabel(value: string | undefined): string {
  if (!value) {
    return 'unknown'
  }
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return 'unknown'
  }
  return date.toLocaleString([], {
    month: 'short',
    day: 'numeric',
    hour: 'numeric',
    minute: '2-digit',
  })
}

function formatPercent(value: number | undefined): string {
  if (value == null || Number.isNaN(value)) {
    return '--'
  }
  return `${Math.round(value * 100)}%`
}

function toneFromStatus(status: string): Tone {
  const lowered = status.toLowerCase()
  if (lowered === 'ok' || lowered === 'available' || lowered === 'ready') {
    return 'healthy'
  }
  if (lowered === 'degraded' || lowered === 'stale' || lowered === 'unknown') {
    return 'warning'
  }
  if (lowered === 'error' || lowered === 'unavailable' || lowered === 'failed') {
    return 'critical'
  }
  return 'neutral'
}

function buildCameraCards(
  cameras: KoalaCamera[],
  ingest: KoalaIngestStatus,
  baseUrl: string,
  token: string,
): CameraCard[] {
  return cameras.map((camera) => {
    const ingestCamera = ingest.cameras[camera.id]
    const captureLabel = formatIsoLabel(ingestCamera?.last_capture_at)
    const detailParts = [
      ingestCamera?.last_error ? `Last error: ${ingestCamera.last_error}` : undefined,
      captureLabel !== 'unknown' ? `Last capture ${captureLabel}` : undefined,
      ingestCamera ? `${ingestCamera.consecutive_failures} consecutive failures` : undefined,
    ].filter(Boolean)

    return {
      id: camera.id,
      name: camera.name || camera.id,
      zoneLabel: camera.zone_id,
      statusLabel: ingestCamera?.last_status || camera.status,
      detail: detailParts.join(' · ') || 'Camera reachable with no recent incidents.',
      tone: toneFromStatus(ingestCamera?.last_status || camera.status),
      snapshotUrl: baseUrl ? `${baseUrl}/admin/cameras/${camera.id}/snapshot?token=${encodeURIComponent(token)}` : undefined,
    }
  })
}

function buildStats(
  health: KoalaHealth,
  cameras: KoalaCamera[],
  packageState: KoalaPackageState,
): StatItem[] {
  const online = cameras.filter((camera) => camera.status === 'available').length
  return [
    {
      label: 'System',
      value: health.status,
      detail: `Inference ${health.inference} · MCP ${health.mcp}`,
      tone: toneFromStatus(health.status),
    },
    {
      label: 'Package',
      value: packageState.package_present ? 'Detected' : 'Clear',
      detail: `Confidence ${formatPercent(packageState.confidence)}`,
      tone: packageState.package_present ? 'warning' : 'healthy',
    },
    {
      label: 'Cameras',
      value: `${online}/${cameras.length} online`,
      detail: 'Current live roster reachability',
      tone: online === cameras.length ? 'healthy' : 'warning',
    },
  ]
}

function buildActivity(
  zoneState: KoalaZoneState,
  packageState: KoalaPackageState,
  ingest: KoalaIngestStatus,
): ActivityItem[] {
  const items: ActivityItem[] = [
    {
      id: `package-${packageState.observed_at}`,
      title: packageState.package_present ? 'Package detected' : 'Front door clear',
      body: `Observed ${formatIsoLabel(packageState.observed_at)} with ${formatPercent(packageState.confidence)} confidence.`,
      timeLabel: formatIsoLabel(packageState.observed_at),
      tone: packageState.package_present ? 'warning' : 'healthy',
      saveKey: `package-${packageState.observed_at}`,
    },
    {
      id: `zone-${zoneState.observed_at}`,
      title: 'Front door zone refreshed',
      body: `${zoneState.entities.length} entities detected in ${zoneState.zone_id}.`,
      timeLabel: formatIsoLabel(zoneState.observed_at),
      tone: zoneState.stale ? 'warning' : 'healthy',
      saveKey: `zone-${zoneState.observed_at}`,
    },
  ]

  for (const incident of ingest.incidents.slice(0, 6)) {
    items.push({
      id: `${incident.camera_id}-${incident.occurred_at}-${incident.type}`,
      title: `${incident.camera_id}: ${incident.type.replaceAll('_', ' ')}`,
      body: incident.message,
      timeLabel: formatIsoLabel(incident.occurred_at),
      tone: toneFromStatus(incident.severity),
      saveKey: `${incident.camera_id}-${incident.occurred_at}-${incident.type}`,
    })
  }

  return items
}

export function getDefaultSettings(): KoalaSettings {
  return {
    baseUrl: import.meta.env.VITE_KOALA_API_BASE_URL ?? '',
    token: import.meta.env.VITE_KOALA_TOKEN ?? '',
    viewerName: 'Home',
    notificationsEnabled: true,
  }
}

export function createKoalaLiveClient(settings: KoalaSettings) {
  const baseUrl = normalizeBaseUrl(settings.baseUrl)

  async function postTool<T>(tool: string, input: Record<string, unknown> = {}) {
    return requestJson<ToolEnvelope<T>>(`${baseUrl}/mcp/tools/${tool}`, {
      method: 'POST',
      headers: buildHeaders(settings.token),
      body: JSON.stringify({ input }),
    })
  }

  return {
    async loadDashboard(): Promise<DashboardSnapshot> {
      if (!canRequest(baseUrl)) {
        return previewSnapshot
      }

      const [health, cameras, zoneState, packageState, ingest] = await Promise.all([
        postTool<KoalaHealth>('koala.get_system_health'),
        postTool<{ cameras: KoalaCamera[] }>('koala.list_cameras'),
        postTool<KoalaZoneState>('koala.get_zone_state', { zone_id: 'front_door' }),
        postTool<KoalaPackageState>('koala.check_package_at_door'),
        requestJson<ToolEnvelope<KoalaIngestStatus>>(`${baseUrl}/admin/ingest/status`, {
          method: 'GET',
          headers: buildHeaders(settings.token, false),
        }),
      ])

      const cameraCards = buildCameraCards(cameras.data.cameras, ingest.data, baseUrl, settings.token)
      const stats = buildStats(health.data, cameras.data.cameras, packageState.data)
      const activity = buildActivity(zoneState.data, packageState.data, ingest.data)

      return {
        headline: packageState.data.package_present
          ? 'Package waiting at the door'
          : 'Home looks calm right now',
        subheadline:
          health.status === 'ok'
            ? 'Koala Live is connected to the current home security state.'
            : 'Koala is reachable but parts of the system are degraded.',
        packageSummary: packageState.data.package_present
          ? `Package detected with ${formatPercent(packageState.data.confidence)} confidence.`
          : 'No package detected at the front door.',
        zoneSummary: `${zoneState.data.entities.length} entities tracked in ${zoneState.data.zone_id}.`,
        serviceLabel: health.status,
        serviceTone: toneFromStatus(health.status),
        stats,
        cameras: cameraCards,
        activity,
        lastUpdatedLabel: formatIsoLabel(zoneState.data.observed_at),
      }
    },

    async checkPackage(): Promise<ActivityItem> {
      if (!canRequest(baseUrl)) {
        return {
          id: 'preview-package-check',
          title: 'Preview package check',
          body: 'Connect a Koala service to run a live package check.',
          timeLabel: 'preview',
          tone: 'neutral',
          saveKey: 'preview-package-check',
        }
      }

      const packageState = await postTool<KoalaPackageState>('koala.check_package_at_door')
      return {
        id: crypto.randomUUID(),
        title: packageState.data.package_present ? 'Package detected' : 'Package check clear',
        body: `Observed ${formatIsoLabel(packageState.data.observed_at)} with ${formatPercent(packageState.data.confidence)} confidence.`,
        timeLabel: formatIsoLabel(packageState.data.observed_at),
        tone: packageState.data.package_present ? 'warning' : 'healthy',
        saveKey: `package-${packageState.data.observed_at}`,
      }
    },
  }
}
