import { useLayoutEffect, type RefObject } from 'react';

// 弹层打开期间跟踪真实位置；工具条换行和动画只改变位置时，ResizeObserver 不会通知。
export function useAnchorPosition(
  open: boolean,
  anchorRef: RefObject<HTMLElement | null>,
  updatePosition: (rect: DOMRect) => void,
) {
  useLayoutEffect(() => {
    if (!open) return;
    let frame = 0;
    let previous: { rect: DOMRect; width: number; height: number } | undefined;
    const update = () => {
      const anchor = anchorRef.current;
      if (!anchor) return;
      const rect = anchor.getBoundingClientRect();
      const width = window.innerWidth;
      const height = window.innerHeight;
      if (!previous || rect.x !== previous.rect.x || rect.y !== previous.rect.y
        || rect.width !== previous.rect.width || rect.height !== previous.rect.height
        || width !== previous.width || height !== previous.height) {
        previous = { rect, width, height };
        updatePosition(rect);
      }
    };
    const track = () => { update(); frame = requestAnimationFrame(track); };
    track();
    // 父容器换行在尺寸通知阶段即可同步；逐帧检查补充尺寸不变的位移和动画。
    const observer = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(update);
    for (let element = anchorRef.current; element; element = element.parentElement) observer?.observe(element);
    window.addEventListener('resize', update);
    window.addEventListener('scroll', update, true);
    return () => {
      cancelAnimationFrame(frame);
      observer?.disconnect();
      window.removeEventListener('resize', update);
      window.removeEventListener('scroll', update, true);
    };
  }, [open, anchorRef, updatePosition]);
}
