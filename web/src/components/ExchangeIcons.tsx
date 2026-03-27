import React from 'react'

interface IconProps {
  width?: number
  height?: number
  className?: string
}

// 本地图标路径映射
const ICON_PATHS: Record<string, string> = {
  binance: '/exchange-icons/binance.jpg',
  bybit: '/exchange-icons/bybit.png',
  okx: '/exchange-icons/okx.svg',
  bitget: '/exchange-icons/bitget.svg',
  gate: '/exchange-icons/gate.svg',
  kucoin: '/exchange-icons/kucoin.svg',
  hyperliquid: '/exchange-icons/hyperliquid.png',
  aster: '/exchange-icons/aster.svg',
  lighter: '/exchange-icons/lighter.png',
}

// 通用图标组件
const ExchangeImage: React.FC<IconProps & { src: string; alt: string }> = ({
  width = 24,
  height = 24,
  className,
  src,
  alt,
}) => (
  <div
    className={className}
    style={{
      width,
      height,
      borderRadius: 6,
      overflow: 'hidden',
      flexShrink: 0,
      background: '#2B3139',
    }}
  >
    <img
      src={src}
      alt={alt}
      style={{
        width: '100%',
        height: '100%',
        objectFit: 'cover',
      }}
    />
  </div>
)

// Fallback 图标
const FallbackIcon: React.FC<IconProps & { label: string }> = ({
  width = 24,
  height = 24,
  className,
  label,
}) => (
  <div
    className={className}
    style={{
      width,
      height,
      borderRadius: 6,
      background: '#2B3139',
      display: 'flex',
      alignItems: 'center',
      justifyContent: 'center',
      fontSize: Math.max(10, (width || 24) * 0.4),
      fontWeight: 'bold',
      color: '#EAECEF',
      flexShrink: 0,
    }}
  >
    {label[0]?.toUpperCase() || '?'}
  </div>
)

// 获取交易所图标的函数
export const getExchangeIcon = (
  exchangeType: string,
  props: IconProps = {}
) => {
  const lowerType = exchangeType.toLowerCase()
  const type = lowerType.includes('binance')
    ? 'binance'
    : lowerType.includes('bybit')
      ? 'bybit'
      : lowerType.includes('okx')
        ? 'okx'
        : lowerType.includes('bitget')
          ? 'bitget'
          : lowerType.includes('gate')
            ? 'gate'
            : lowerType.includes('kucoin')
              ? 'kucoin'
              : lowerType.includes('hyperliquid')
                ? 'hyperliquid'
                : lowerType.includes('aster')
                  ? 'aster'
                  : lowerType.includes('lighter')
                    ? 'lighter'
                    : lowerType

  const iconProps = {
    width: props.width || 24,
    height: props.height || 24,
    className: props.className,
  }

  // Paper trading icon - use a styled fallback with distinct color
  if (type === 'paper') {
    return (
      <div
        className={iconProps.className}
        style={{
          width: iconProps.width,
          height: iconProps.height,
          borderRadius: 6,
          background: 'linear-gradient(135deg, #3B82F6, #1D4ED8)',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          fontSize: Math.max(10, (iconProps.width || 24) * 0.45),
          flexShrink: 0,
          color: '#fff',
        }}
      >
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={{ width: '60%', height: '60%' }}>
          <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" />
          <polyline points="14 2 14 8 20 8" />
          <line x1="16" y1="13" x2="8" y2="13" />
          <line x1="16" y1="17" x2="8" y2="17" />
        </svg>
      </div>
    )
  }

  const path = ICON_PATHS[type]
  if (path) {
    return <ExchangeImage {...iconProps} src={path} alt={type} />
  }

  return <FallbackIcon {...iconProps} label={type} />
}
