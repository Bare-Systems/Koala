import type { ReactNode } from 'react'
import type { ActivityItem, CameraCard, LiveTab, Tone } from '../lib/types'

const tabs: Array<{ id: LiveTab; label: string; eyebrow: string }> = [
  { id: 'home', label: 'Home', eyebrow: 'Live' },
  { id: 'activity', label: 'Activity', eyebrow: 'Alerts' },
  { id: 'cameras', label: 'Cameras', eyebrow: 'Views' },
  { id: 'profile', label: 'Profile', eyebrow: 'Household' },
]

type TabBarProps = {
  activeTab: LiveTab
  onSelect: (tab: LiveTab) => void
}

export function TabBar({ activeTab, onSelect }: TabBarProps) {
  return (
    <nav className="tab-bar" aria-label="Koala Live sections">
      {tabs.map((tab) => (
        <button
          key={tab.id}
          className={tab.id === activeTab ? 'tab-chip tab-chip-active' : 'tab-chip'}
          onClick={() => onSelect(tab.id)}
          type="button"
        >
          <span>{tab.eyebrow}</span>
          <strong>{tab.label}</strong>
        </button>
      ))}
    </nav>
  )
}

type PanelProps = {
  eyebrow: string
  title: string
  subtitle: string
  children: ReactNode
}

export function Panel({ eyebrow, title, subtitle, children }: PanelProps) {
  return (
    <section className="panel">
      <header className="panel-header">
        <p>{eyebrow}</p>
        <h2>{title}</h2>
        <span>{subtitle}</span>
      </header>
      {children}
    </section>
  )
}

type StatusPillProps = {
  label: string
  tone: Tone
}

export function StatusPill({ label, tone }: StatusPillProps) {
  return <span className={`status-pill tone-${tone}`}>{label}</span>
}

type StatCardProps = {
  label: string
  value: string
  detail: string
  tone: Tone
}

export function StatCard({ label, value, detail, tone }: StatCardProps) {
  return (
    <article className={`stat-card tone-${tone}`}>
      <span>{label}</span>
      <strong>{value}</strong>
      <p>{detail}</p>
    </article>
  )
}

type CameraCardProps = {
  camera: CameraCard
}

export function CameraCardView({ camera }: CameraCardProps) {
  return (
    <article className={`camera-card tone-${camera.tone}`}>
      <header>
        <div>
          <strong>{camera.name}</strong>
          <span>{camera.zoneLabel}</span>
        </div>
        <StatusPill label={camera.statusLabel} tone={camera.tone} />
      </header>
      <p>{camera.detail}</p>
    </article>
  )
}

type ActivityListProps = {
  items: ActivityItem[]
  savedKeys: Set<string>
  onToggleSave: (item: ActivityItem) => void
}

export function ActivityList({ items, savedKeys, onToggleSave }: ActivityListProps) {
  return (
    <div className="activity-list">
      {items.map((item) => {
        const isSaved = savedKeys.has(item.saveKey)
        return (
          <article key={item.id} className={`activity-card tone-${item.tone}`}>
            <div className="activity-line" />
            <div className="activity-body">
              <header>
                <div>
                  <strong>{item.title}</strong>
                  <span>{item.timeLabel}</span>
                </div>
                <button className="ghost-button" onClick={() => onToggleSave(item)} type="button">
                  {isSaved ? 'Saved' : 'Save'}
                </button>
              </header>
              <p>{item.body}</p>
            </div>
          </article>
        )
      })}
    </div>
  )
}

type ToggleRowProps = {
  label: string
  detail: string
  checked: boolean
  onChange: (value: boolean) => void
}

export function ToggleRow({ label, detail, checked, onChange }: ToggleRowProps) {
  return (
    <label className="toggle-row">
      <div>
        <strong>{label}</strong>
        <span>{detail}</span>
      </div>
      <input
        checked={checked}
        onChange={(event) => onChange(event.target.checked)}
        type="checkbox"
      />
    </label>
  )
}
