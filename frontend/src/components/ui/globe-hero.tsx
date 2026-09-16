// DotGlobeHero — verilen shadcn bileseninin bu kod tabanina UYARLANMIS hali.
//
// Kaynaga gore DORT DEGISIKLIK, her birinin gerekcesi yerinde:
//   1) `"use client"` KALDIRILDI — Next.js yonergesi; Vite SPA'de anlamsiz.
//   2) `color="hsl(var(--foreground))"` DUZELTILDI — bu deger THREE'ye gidiyordu;
//      THREE renkleri kendi ayristirir ve CSS degiskenini COZEMEZ, sessizce
//      beyaza duserdi. Renk artik disaridan gercek bir deger olarak gelir.
//   3) shadcn tokenlari (`bg-background` vb.) KALDIRILDI — bu projede tanimli
//      degiller, Tailwind onlar icin hicbir kural uretmez.
//   4) `h-screen` -> `h-full` — bilesen sayfanin tamami degil, grid sutunu.
import { Canvas, useFrame } from '@react-three/fiber'
import { PerspectiveCamera } from '@react-three/drei'
import React, { useRef } from 'react'
import type * as THREE from 'three'
import { cn } from '@/lib/utils'

export interface DotGlobeHeroProps {
  rotationSpeed?: number
  globeRadius?: number
  /** Tel kafes rengi. GERCEK bir renk olmali (#rrggbb / rgb()) — CSS degiskeni CALISMAZ. */
  color?: string
  opacity?: number
  /** Kure bolutlemesi. Kaynakta 64 sabitti; 64 tel kafeste bulanik bir top gibi
   *  gorunuyor, bolut sayisi dustukce kure okunur hale geliyor. */
  segments?: number
  className?: string
  children?: React.ReactNode
}

const Globe: React.FC<{
  rotationSpeed: number
  radius: number
  color: string
  opacity: number
  segments: number
}> = ({ rotationSpeed, radius, color, opacity, segments }) => {
  const groupRef = useRef<THREE.Group>(null!)

  useFrame(() => {
    if (!groupRef.current) return
    groupRef.current.rotation.y += rotationSpeed
    groupRef.current.rotation.x += rotationSpeed * 0.3
    groupRef.current.rotation.z += rotationSpeed * 0.1
  })

  return (
    <group ref={groupRef}>
      <mesh>
        <sphereGeometry args={[radius, segments, segments]} />
        <meshBasicMaterial color={color} transparent opacity={opacity} wireframe />
      </mesh>
    </group>
  )
}

const DotGlobeHero = React.forwardRef<HTMLDivElement, DotGlobeHeroProps>(
  (
    {
      rotationSpeed = 0.005,
      globeRadius = 1,
      color = '#3b82f6',
      opacity = 0.15,
      segments = 48,
      className,
      children,
      ...props
    },
    ref,
  ) => {
    return (
      <div ref={ref} className={cn('relative w-full h-full overflow-hidden', className)} {...props}>
        <div className="relative z-10 flex h-full flex-col items-center justify-center">
          {children}
        </div>

        <div className="pointer-events-none absolute inset-0 z-0">
          {/* dpr ust sinirlanir: 3x ekranlarda tam cozunurluk bu dekoratif sahne
              icin GPU'yu bosuna yorar. powerPreference low-power ayrik ekran
              kartini uyandirmaz (dizustunde pil). */}
          <Canvas dpr={[1, 2]} gl={{ antialias: true, powerPreference: 'low-power' }}>
            <PerspectiveCamera makeDefault position={[0, 0, 3]} fov={75} />
            <ambientLight intensity={0.5} />
            <pointLight position={[10, 10, 10]} intensity={1} />
            <Globe
              rotationSpeed={rotationSpeed}
              radius={globeRadius}
              color={color}
              opacity={opacity}
              segments={segments}
            />
          </Canvas>
        </div>
      </div>
    )
  },
)

DotGlobeHero.displayName = 'DotGlobeHero'

export { DotGlobeHero }
