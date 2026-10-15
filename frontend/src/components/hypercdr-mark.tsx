export function HyperCDRMark({ className = "hypercdr-mark" }: { className?: string }) {
 return (
<svg viewBox="0 0 128 128" aria-hidden="true" className={className}>
  <defs>
    <linearGradient id="hcdr-mark-frame" x1="24" y1="28" x2="104" y2="100" gradientUnits="userSpaceOnUse">
      <stop offset="0" stopColor="#ffcf3d" />
      <stop offset="0.48" stopColor="#67e8f9" />
      <stop offset="1" stopColor="#6d7cff" />
    </linearGradient>
    <linearGradient id="hcdr-mark-core" x1="42" y1="42" x2="88" y2="88" gradientUnits="userSpaceOnUse">
      <stop offset="0" stopColor="#67e8f9" />
      <stop offset="1" stopColor="#ffcf3d" />
    </linearGradient>
    <filter id="hcdr-mark-soft-glow" x="-25%" y="-25%" width="150%" height="150%">
      <feGaussianBlur stdDeviation="4" result="blur" />
      <feColorMatrix in="blur" type="matrix" values="0 0 0 0 0.35 0 0 0 0 0.68 0 0 0 0 1 0 0 0 0.50 0" />
      <feMerge>
        <feMergeNode />
        <feMergeNode in="SourceGraphic" />
      </feMerge>
    </filter>
  </defs>
  <path d="M64 18 101 39v50l-37 21-37-21V39l37-21Z" fill="rgba(7,10,18,0.84)" stroke="url(#hcdr-mark-frame)" strokeWidth="5" filter="url(#hcdr-mark-soft-glow)" />
  <rect x="47" y="45" width="15" height="15" rx="4" fill="#67e8f9" />
  <rect x="66" y="45" width="15" height="15" rx="4" fill="#ffffff" opacity="0.92" />
  <rect x="47" y="64" width="15" height="15" rx="4" fill="#ffffff" opacity="0.92" />
  <rect x="66" y="64" width="15" height="15" rx="4" fill="#ffcf3d" />
</svg>
 );
}
