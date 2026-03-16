import { startTransition, useCallback, useEffect, useMemo, useState } from 'react'
import './App.css'
import { createKoalaLiveClient, getDefaultSettings } from './api/client'
import { ActivityList, CameraCardView, Panel, StatCard, StatusPill, TabBar, ToggleRow } from './components/ui'
import { previewSnapshot, tabPrompts } from './data/mockData'
import type { ActivityItem, DashboardSnapshot, KoalaSettings, LiveTab } from './lib/types'

const settingsKey = 'koala-live-settings'
const savedMomentsKey = 'koala-live-saved-moments'

function mergeSettings(defaults: KoalaSettings, parsed: Partial<KoalaSettings>): KoalaSettings {
  return {
    ...defaults,
    ...parsed,
  }
}

function App() {
  const [activeTab, setActiveTab] = useState<LiveTab>('home')
  const [settings, setSettings] = useState<KoalaSettings>(() => {
    const defaults = getDefaultSettings()
    const stored = globalThis.localStorage?.getItem(settingsKey)
    if (!stored) {
      return defaults
    }
    try {
      return mergeSettings(defaults, JSON.parse(stored) as Partial<KoalaSettings>)
    } catch {
      return defaults
    }
  })
  const [snapshot, setSnapshot] = useState<DashboardSnapshot>(previewSnapshot)
  const [savedMoments, setSavedMoments] = useState<string[]>(() => {
    const stored = globalThis.localStorage?.getItem(savedMomentsKey)
    if (!stored) {
      return []
    }
    try {
      return JSON.parse(stored) as string[]
    } catch {
      return []
    }
  })
  const [statusText, setStatusText] = useState(
    'Koala Live is in preview mode until a live Koala endpoint is configured.',
  )
  const [isRefreshing, setIsRefreshing] = useState(false)

  useEffect(() => {
    globalThis.localStorage?.setItem(settingsKey, JSON.stringify(settings))
  }, [settings])

  useEffect(() => {
    globalThis.localStorage?.setItem(savedMomentsKey, JSON.stringify(savedMoments))
  }, [savedMoments])

  const client = useMemo(() => createKoalaLiveClient(settings), [settings])
  const savedMomentSet = useMemo(() => new Set(savedMoments), [savedMoments])

  const refreshDashboard = useCallback(async (statusMessage?: string) => {
    setIsRefreshing(true)
    try {
      const nextSnapshot = await client.loadDashboard()
      setSnapshot(nextSnapshot)
      setStatusText(statusMessage ?? `Koala refreshed ${nextSnapshot.lastUpdatedLabel}.`)
    } catch (error) {
      const detail = error instanceof Error ? error.message : 'Unknown error'
      setStatusText(detail)
    } finally {
      setIsRefreshing(false)
    }
  }, [client])

  useEffect(() => {
    void refreshDashboard()
  }, [refreshDashboard])

  const toggleSavedMoment = useCallback((item: ActivityItem) => {
    setSavedMoments((current) =>
      current.includes(item.saveKey)
        ? current.filter((value) => value !== item.saveKey)
        : [item.saveKey, ...current],
    )
  }, [])

  const runPackageCheck = useCallback(async () => {
    setIsRefreshing(true)
    try {
      const event = await client.checkPackage()
      setSnapshot((current) => ({
        ...current,
        activity: [event, ...current.activity].slice(0, 8),
        packageSummary: event.body,
        headline: event.title,
      }))
      setStatusText('Package check completed.')
    } catch (error) {
      const detail = error instanceof Error ? error.message : 'Unknown error'
      setStatusText(detail)
    } finally {
      setIsRefreshing(false)
    }
  }, [client])

  function renderMainPanel() {
    switch (activeTab) {
      case 'home':
        return (
          <div className="content-grid">
            <Panel
              eyebrow="Live Home"
              title={snapshot.headline}
              subtitle={snapshot.subheadline}
            >
              <div className="prompt-row">
                {tabPrompts.map((prompt) => (
                  <div key={prompt} className="prompt-pill">
                    {prompt}
                  </div>
                ))}
              </div>
              <div className="stats-grid">
                {snapshot.stats.map((stat) => (
                  <StatCard
                    key={stat.label}
                    label={stat.label}
                    value={stat.value}
                    detail={stat.detail}
                    tone={stat.tone}
                  />
                ))}
              </div>
              <div className="action-row">
                <button className="primary-button" onClick={() => void refreshDashboard('Koala Live refreshed.')} type="button">
                  {isRefreshing ? 'Refreshing…' : 'Refresh live status'}
                </button>
                <button className="secondary-button" onClick={() => void runPackageCheck()} type="button">
                  Check package
                </button>
              </div>
            </Panel>

            <div className="stack">
              <Panel
                eyebrow="Front Door"
                title="Doorstep summary"
                subtitle="The most consumer-friendly reading of the current front door state."
              >
                <p className="summary-copy">{snapshot.packageSummary}</p>
                <p className="summary-copy summary-copy-muted">{snapshot.zoneSummary}</p>
              </Panel>

              <Panel
                eyebrow="Saved"
                title="Saved moments"
                subtitle="Stored locally until Koala recording and consumer-save APIs exist."
              >
                <div className="saved-count">
                  <strong>{savedMoments.length}</strong>
                  <span>moments saved on this device</span>
                </div>
              </Panel>
            </div>
          </div>
        )
      case 'activity':
        return (
          <div className="content-grid">
            <Panel
              eyebrow="Timeline"
              title="Recent activity"
              subtitle="Incidents, package checks, and front door state updates from Koala."
            >
              <ActivityList
                items={snapshot.activity}
                savedKeys={savedMomentSet}
                onToggleSave={toggleSavedMoment}
              />
            </Panel>

            <Panel
              eyebrow="Notes"
              title="What save means today"
              subtitle="Consumer save behavior exists in the UI even before recording endpoints do."
            >
              <ul className="info-list">
                <li>Saved moments are local to this browser for now.</li>
                <li>Once Koala recording APIs exist, this can switch to real cloud or device persistence.</li>
                <li>Live camera playback is intentionally deferred until the media path is ready.</li>
              </ul>
            </Panel>
          </div>
        )
      case 'cameras':
        return (
          <div className="content-grid">
            <Panel
              eyebrow="Roster"
              title="Camera views"
              subtitle="Current consumer-friendly camera availability and status."
            >
              <div className="camera-grid">
                {snapshot.cameras.map((camera) => (
                  <CameraCardView key={camera.id} camera={camera} />
                ))}
              </div>
            </Panel>

            <Panel
              eyebrow="Playback"
              title="Media path status"
              subtitle="Koala Live is ready for camera surfaces, but full playback waits on the feed pipeline."
            >
              <ul className="info-list">
                <li>Use this screen as the camera roster and health surface for now.</li>
                <li>Consumer playback and clip retrieval will slot in here once available.</li>
                <li>The first deployment target remains Docker on blink, then Jetson later.</li>
              </ul>
            </Panel>
          </div>
        )
      case 'profile':
        return (
          <div className="content-grid">
            <Panel
              eyebrow="Household"
              title="Profile and preferences"
              subtitle="Consumer-facing writes stay narrow and safe."
            >
              <div className="field-grid">
                <label className="field">
                  <span>Viewer name</span>
                  <input
                    value={settings.viewerName}
                    onChange={(event) =>
                      setSettings((current) => ({
                        ...current,
                        viewerName: event.target.value,
                      }))
                    }
                    placeholder="Home"
                  />
                </label>
              </div>
              <ToggleRow
                label="Critical notifications"
                detail="Stored locally until consumer profile APIs exist."
                checked={settings.notificationsEnabled}
                onChange={(value) =>
                  setSettings((current) => ({
                    ...current,
                    notificationsEnabled: value,
                  }))
                }
              />
            </Panel>

            <Panel
              eyebrow="Service"
              title="Connectivity"
              subtitle="Useful for local development and kiosk deployment."
            >
              <div className="field-grid">
                <label className="field">
                  <span>Koala URL</span>
                  <input
                    value={settings.baseUrl}
                    onChange={(event) =>
                      setSettings((current) => ({
                        ...current,
                        baseUrl: event.target.value,
                      }))
                    }
                    placeholder="https://koala.example.com"
                  />
                </label>
                <label className="field">
                  <span>Bearer token</span>
                  <input
                    value={settings.token}
                    onChange={(event) =>
                      setSettings((current) => ({
                        ...current,
                        token: event.target.value,
                      }))
                    }
                    placeholder="Consumer-safe token"
                  />
                </label>
              </div>
            </Panel>
          </div>
        )
      default:
        return null
    }
  }

  return (
    <div className="app-shell">
      <header className="hero-shell">
        <div>
          <p className="hero-kicker">Koala Live</p>
          <h1>{settings.viewerName || 'Home'} security at a glance.</h1>
          <p className="hero-copy">
            Koala Live is the consumer-facing home monitor for camera state, package checks,
            and recent security activity.
          </p>
        </div>
        <div className="hero-meta">
          <StatusPill label={snapshot.serviceLabel} tone={snapshot.serviceTone} />
          <p>{statusText}</p>
          <span>Last updated {snapshot.lastUpdatedLabel}</span>
        </div>
      </header>

      <TabBar
        activeTab={activeTab}
        onSelect={(tab) => {
          startTransition(() => {
            setActiveTab(tab)
          })
        }}
      />

      {renderMainPanel()}
    </div>
  )
}

export default App
