"use client";

import { useEffect, useRef, useState } from "react";

/**
 * 为「进入 / 退场」动画管理挂载状态。
 *
 * 问题：`{open && <div>}` 在 open 变 false 时立刻卸载元素，
 * 退场动画根本没机会播。CSS 无法解决——元素已经不在 DOM 里了。
 *
 * 做法：open 变 false 后先切到 closing 状态（此时挂退场动画类），
 * 等动画时长过去再真正卸载。open 变 true 时立即挂载并播进入动画。
 *
 * 为什么不用 framer-motion：整站只加 ~30KB 换这几个退场动画不划算，
 * 而这里需要的逻辑只有「延迟卸载」一件事，十行代码就够。
 *
 * @param open     外部控制的开合状态
 * @param duration 退场动画时长（毫秒），需与 CSS 保持一致
 */
export function usePresence(open: boolean, duration = 100) {
  const [mounted, setMounted] = useState(open);
  const [closing, setClosing] = useState(false);
  const timer = useRef<number | null>(null);
  const mountedRef = useRef(open);

  useEffect(() => {
    // 打开：立即挂载并清掉 closing，让进入动画从 from 帧开始播。
    if (open) {
      setMounted(true);
      setClosing(false);
      return;
    }

    // 关闭：若当前没挂载就什么都不做（首帧 open=false 时不会触发退场）。
    // 用 ref 读挂载状态而不是 setState 回调：在更新函数里调 setClosing
    // 属于副作用，StrictMode 下会执行两次，导致定时器被建两遍。
    if (!mountedRef.current) return;
    setClosing(true);
    timer.current = window.setTimeout(() => {
      setClosing(false);
      setMounted(false);
      timer.current = null;
    }, duration);

    return () => {
      if (timer.current !== null) {
        window.clearTimeout(timer.current);
        timer.current = null;
      }
    };
  }, [open, duration]);

  // 同步 ref，供上面的 effect 判断「是否已挂载」。
  mountedRef.current = mounted;

  return { mounted, closing };
}

/**
 * 弹窗焦点陷阱。
 *
 * 原生 <dialog>.showModal() 自带焦点陷阱，但自绘遮罩的弹窗没有。
 * 这里补上三件事：
 *   1. 打开时把焦点移到弹窗内第一个可聚焦元素；
 *   2. Tab / Shift+Tab 在弹窗内循环，不跑到背后的页面；
 *   3. 关闭后把焦点还给打开前的元素（否则键盘用户会迷失在页面顶部）。
 *
 * 为什么要还给原元素：用户在「认证」按钮上按回车打开弹窗，
 * 关闭后焦点若落到 body，下一次 Tab 会从页面开头重新数起。
 */
export function useFocusTrap(active: boolean, containerRef: React.RefObject<HTMLElement | null>) {
  useEffect(() => {
    if (!active) return;
    const container = containerRef.current;
    if (!container) return;

    const previous = document.activeElement as HTMLElement | null;

    const selector =
      'a[href], button:not([disabled]), textarea:not([disabled]), input:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])';

    const focusables = () =>
      Array.from(container.querySelectorAll<HTMLElement>(selector)).filter(
        // 排除不可见元素（display:none 的元素仍在 DOM 里，但聚焦会失败）。
        (el) => el.offsetParent !== null || el === document.activeElement,
      );

    // 打开时聚焦第一个可聚焦元素；没有则聚焦容器本身。
    const first = focusables()[0];
    if (first) {
      first.focus();
    } else {
      container.setAttribute("tabindex", "-1");
      container.focus();
    }

    function onKeyDown(event: KeyboardEvent) {
      if (event.key !== "Tab") return;
      const items = focusables();
      if (items.length === 0) {
        event.preventDefault();
        return;
      }
      const firstEl = items[0];
      const lastEl = items[items.length - 1];
      const current = document.activeElement;

      if (event.shiftKey) {
        // Shift+Tab 在第一个元素上 → 绕到最后一个。
        if (current === firstEl || !container!.contains(current)) {
          event.preventDefault();
          lastEl.focus();
        }
      } else {
        // Tab 在最后一个元素上 → 绕回第一个。
        if (current === lastEl || !container!.contains(current)) {
          event.preventDefault();
          firstEl.focus();
        }
      }
    }

    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("keydown", onKeyDown);
      // 焦点归还：元素可能已经不在 DOM 里（例如它自己触发卸载），
      // 这时退回到 body，不要抛错。
      if (previous && document.contains(previous)) {
        previous.focus();
      }
    };
  }, [active, containerRef]);
}
