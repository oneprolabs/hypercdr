export function followTimeline(timeline: HTMLElement): () => void {
  let container = timeline.parentElement;
  while (container && !/(auto|scroll)/.test(getComputedStyle(container).overflowY)) {
    container = container.parentElement;
  }
  if (!container) return () => {};
  const viewport = container;
  let following = true;
  let frame = 0;
  const onScroll = () => {
    following = viewport.scrollHeight - viewport.clientHeight - viewport.scrollTop <= 48;
  };
  const follow = () => {
    if (!following) return;
    cancelAnimationFrame(frame);
    frame = requestAnimationFrame(() => {
      if (following) viewport.scrollTop = viewport.scrollHeight;
    });
  };
  const observer = new ResizeObserver(follow);
  observer.observe(timeline);
  viewport.addEventListener('scroll', onScroll, { passive: true });
  follow();
  return () => {
    cancelAnimationFrame(frame);
    observer.disconnect();
    viewport.removeEventListener('scroll', onScroll);
  };
}
