import { useCallback, useEffect, useRef, useState } from 'react'
import { cn } from '@/lib/utils'

// One full drop → splash → ripple cycle; keep in sync with `.tp-logo` in index.css.
const CYCLE_MS = 2800

export type TidepoolLogoMode = 'loop' | 'hover' | 'static'

type TidepoolLogoProps = {
  size?: number
  /**
   * loop: animate forever (login hero).
   * hover: play once on mount and once each time the pointer enters the nearest
   *        `[data-logo-host]` ancestor (sidebar), so it never distracts.
   * static: still frame only.
   */
  mode?: TidepoolLogoMode
  className?: string
  title?: string
}

/**
 * The default Tidepool mark: a drop falls into the pool and elliptical ripples
 * spread out. Colour follows `currentColor`, so callers set it with `text-*`.
 * `prefers-reduced-motion` always freezes it on the still frame (see index.css).
 */
export function TidepoolLogo({ size = 28, mode = 'static', className, title }: TidepoolLogoProps) {
  const ref = useRef<SVGSVGElement>(null)
  const timer = useRef<number | undefined>(undefined)
  const [playingOnce, setPlayingOnce] = useState(false)

  const playOnce = useCallback(() => {
    if (timer.current !== undefined) return
    setPlayingOnce(true)
    timer.current = window.setTimeout(() => {
      timer.current = undefined
      setPlayingOnce(false)
    }, CYCLE_MS + 100)
  }, [])

  useEffect(() => {
    if (mode !== 'hover') return undefined
    playOnce()
    const host = ref.current?.closest('[data-logo-host]') ?? ref.current
    host?.addEventListener('mouseenter', playOnce)
    return () => {
      host?.removeEventListener('mouseenter', playOnce)
      if (timer.current !== undefined) window.clearTimeout(timer.current)
      timer.current = undefined
    }
  }, [mode, playOnce])

  const playing = mode === 'loop' || (mode === 'hover' && playingOnce)
  const strokeWidth = size >= 60 ? 2.4 : 2

  return (
    <svg
      ref={ref}
      className={cn('tp-logo', playing && 'is-playing', mode === 'hover' && 'play-once', className)}
      width={size}
      height={size}
      viewBox="0 0 32 32"
      role={title ? 'img' : undefined}
      aria-hidden={title ? undefined : true}
      aria-label={title}
    >
      {[1, 2, 3].map((n) => (
        <ellipse
          key={n}
          className={`tp-ring tp-r${n}`}
          cx="16"
          cy="23.5"
          rx="13.5"
          ry="4.6"
          fill="none"
          stroke="currentColor"
          strokeWidth={strokeWidth}
          vectorEffect="non-scaling-stroke"
        />
      ))}
      <circle className="tp-splash" cx="16" cy="21.5" r="1.3" fill="currentColor" />
      <path
        className="tp-drop"
        d="M16 2.5c3 3.7 4.6 6.2 4.6 8.2a4.6 4.6 0 0 1-9.2 0c0-2 1.6-4.5 4.6-8.2Z"
        fill="currentColor"
      />
    </svg>
  )
}
