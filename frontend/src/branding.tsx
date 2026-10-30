import { createContext, type PropsWithChildren, useCallback, useContext, useEffect, useMemo, useState } from 'react'
import { api } from './api'
import i18n from './i18n'
import type { SiteBranding } from './types'
import defaultLogo from './assets/logo.svg'

// 品牌名的唯一来源；界面上展示品牌时一律读取 useBranding().siteName（运行时站点名称）。
export const DEFAULT_SITE_NAME = 'Tidepool'
// 旧版本数据库默认值；仍按未自定义处理，显示为当前默认名称。
const LEGACY_DEFAULT_SITE_NAMES = new Set(['CodexProxy'])
export const DEFAULT_SITE_LOGO = defaultLogo
const DEFAULT_FAVICON = `${import.meta.env.BASE_URL}favicon.svg`
const DEFAULT_BACKGROUND_GLASS_OPACITY = 58
const DEFAULT_BACKGROUND_GLASS_BLUR = 5

type BrandingContextValue = {
  siteName: string
  siteLogo: string
  backgroundImage: string
  backgroundOpacity: number
  backgroundBlur: number
  backgroundGlassOpacity: number
  backgroundGlassBlur: number
  faviconHref: string
  refreshBranding: () => Promise<void>
  applyBranding: (branding: Partial<SiteBranding>) => void
}

const BrandingContext = createContext<BrandingContextValue | null>(null)

export function sanitizeBrandingLogo(value?: string | null): string {
  return sanitizeBrandingImage(value)
}

export function sanitizeBrandingImage(value?: string | null): string {
  const trimmed = (value ?? '').trim()
  if (!trimmed) return ''
  const lower = trimmed.toLowerCase()
  if (lower.startsWith('data:image/') && lower.includes(';base64,')) return trimmed
  if (lower.startsWith('data:video/mp4') && lower.includes(';base64,')) return trimmed
  if (lower.startsWith('https://') || lower.startsWith('http://')) return trimmed
  if (trimmed.startsWith('/') && !trimmed.startsWith('//')) return trimmed
  return ''
}

export function isBrandingVideo(value?: string | null): boolean {
  const trimmed = (value ?? '').trim().toLowerCase()
  if (!trimmed) return false
  if (trimmed.startsWith('data:video/mp4')) return true
  return /(^|\/)[^/?#]+\.mp4(?:[?#].*)?$/.test(trimmed)
}

function clampPercent(value?: number | null, fallback = 18): number {
  if (typeof value !== 'number' || !Number.isFinite(value)) return fallback
  return Math.min(100, Math.max(0, Math.round(value)))
}

function clampBlur(value?: number | null): number {
  if (typeof value !== 'number' || !Number.isFinite(value)) return 0
  return Math.min(24, Math.max(0, Math.round(value)))
}

function clampGlassBlur(value?: number | null, fallback = DEFAULT_BACKGROUND_GLASS_BLUR): number {
  if (typeof value !== 'number' || !Number.isFinite(value)) return fallback
  return Math.min(20, Math.max(0, Math.round(value)))
}

export function normalizeSiteName(value?: string | null): string {
  const trimmed = (value ?? '').trim()
  if (!trimmed || LEGACY_DEFAULT_SITE_NAMES.has(trimmed)) return DEFAULT_SITE_NAME
  return trimmed
}

// 文案中的 {{siteName}} 由 i18next 默认变量提供，随运行时站点名称更新。
function syncI18nSiteName(siteName: string) {
  i18n.options.interpolation = {
    ...i18n.options.interpolation,
    defaultVariables: { ...i18n.options.interpolation?.defaultVariables, siteName },
  }
}
syncI18nSiteName(DEFAULT_SITE_NAME)

function setIconLink(rel: string, href: string) {
  let link = document.querySelector<HTMLLinkElement>(`link[rel="${rel}"]`)
  if (!link) {
    link = document.createElement('link')
    link.rel = rel
    document.head.appendChild(link)
  }
  link.href = href
}

export function BrandingProvider({ children }: PropsWithChildren) {
  const [branding, setBranding] = useState<SiteBranding>({
    site_name: DEFAULT_SITE_NAME,
    site_logo: '',
    background_image: '',
    background_opacity: 18,
    background_blur: 0,
    background_glass_opacity: DEFAULT_BACKGROUND_GLASS_OPACITY,
    background_glass_blur: DEFAULT_BACKGROUND_GLASS_BLUR,
  })

  const applyBranding = useCallback((next: Partial<SiteBranding>) => {
    setBranding((current) => ({
      site_name: normalizeSiteName(next.site_name ?? current.site_name),
      site_logo: next.site_logo === undefined ? current.site_logo : sanitizeBrandingLogo(next.site_logo),
      background_image: next.background_image === undefined ? current.background_image : sanitizeBrandingImage(next.background_image),
      background_opacity: next.background_opacity === undefined ? current.background_opacity : clampPercent(next.background_opacity, current.background_opacity),
      background_blur: next.background_blur === undefined ? current.background_blur : clampBlur(next.background_blur),
      background_glass_opacity: next.background_glass_opacity === undefined ? current.background_glass_opacity : clampPercent(next.background_glass_opacity, current.background_glass_opacity),
      background_glass_blur: next.background_glass_blur === undefined ? current.background_glass_blur : clampGlassBlur(next.background_glass_blur, current.background_glass_blur),
    }))
  }, [])

  const refreshBranding = useCallback(async () => {
    try {
      const next = await api.getBranding()
      applyBranding(next)
    } catch {
      applyBranding({})
    }
  }, [applyBranding])

  useEffect(() => {
    void refreshBranding()
  }, [refreshBranding])

  const siteName = normalizeSiteName(branding.site_name)
  const siteLogo = sanitizeBrandingLogo(branding.site_logo)
  const backgroundImage = sanitizeBrandingImage(branding.background_image)
  const backgroundOpacity = clampPercent(branding.background_opacity)
  const backgroundBlur = clampBlur(branding.background_blur)
  const backgroundGlassOpacity = clampPercent(branding.background_glass_opacity, DEFAULT_BACKGROUND_GLASS_OPACITY)
  const backgroundGlassBlur = clampGlassBlur(branding.background_glass_blur)
  const faviconHref = siteLogo || DEFAULT_FAVICON

  useEffect(() => {
    syncI18nSiteName(siteName)
    document.title = siteName
    setIconLink('icon', faviconHref)
    // iOS ignores SVG touch icons, so the default stays on the PNG render.
    setIconLink('apple-touch-icon', siteLogo || `${import.meta.env.BASE_URL}favicon.png`)
  }, [faviconHref, siteLogo, siteName])

  const value = useMemo<BrandingContextValue>(() => ({
    siteName,
    siteLogo,
    backgroundImage,
    backgroundOpacity,
    backgroundBlur,
    backgroundGlassOpacity,
    backgroundGlassBlur,
    faviconHref,
    refreshBranding,
    applyBranding,
  }), [applyBranding, backgroundBlur, backgroundGlassBlur, backgroundGlassOpacity, backgroundImage, backgroundOpacity, faviconHref, refreshBranding, siteLogo, siteName])

  return (
    <BrandingContext.Provider value={value}>
      {children}
    </BrandingContext.Provider>
  )
}

export function useBranding() {
  const context = useContext(BrandingContext)
  if (!context) {
    throw new Error('useBranding must be used inside BrandingProvider')
  }
  return context
}
