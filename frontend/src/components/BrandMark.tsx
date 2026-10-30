import { useBranding } from '../branding'
import { cn } from '@/lib/utils'
import { TidepoolLogo, type TidepoolLogoMode } from './TidepoolLogo'

type BrandMarkProps = {
  /** Rendered box size in px. */
  size: number
  mode?: TidepoolLogoMode
  className?: string
}

/**
 * Site logo slot. A logo uploaded in Settings wins; otherwise the animated
 * Tidepool mark is drawn in the theme's primary colour.
 */
export function BrandMark({ size, mode = 'static', className }: BrandMarkProps) {
  const { siteName, siteLogo } = useBranding()
  if (siteLogo) {
    return (
      <img
        src={siteLogo}
        alt={siteName}
        width={size}
        height={size}
        className={cn('shrink-0 rounded-lg object-cover', className)}
        style={{ width: size, height: size }}
      />
    )
  }
  return (
    <span
      className={cn('inline-flex shrink-0 items-center justify-center text-primary', className)}
      style={{ width: size, height: size }}
    >
      <TidepoolLogo size={Math.round(size * 0.86)} mode={mode} title={siteName} />
    </span>
  )
}
