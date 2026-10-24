import { useEffect, useState } from 'react';

const secondsUntil = (until: number | null) =>
  until === null ? 0 : Math.max(0, Math.ceil((until - Date.now()) / 1000));

/**
 * The whole seconds left until `until` (a Date.now() instant), ticking once a second; 0 once it
 * has passed, or when there is nothing to wait for (null).
 */
export function useSecondsLeft(until: number | null): number {
  const [seconds, setSeconds] = useState(() => secondsUntil(until));
  useEffect(() => {
    setSeconds(secondsUntil(until));
    if (until === null) return;
    const timer = setInterval(() => {
      const next = secondsUntil(until);
      setSeconds(next);
      if (next === 0) clearInterval(timer);
    }, 1000);
    return () => clearInterval(timer);
  }, [until]);
  return seconds;
}

/** "0:24" */
export const minutesAndSeconds = (seconds: number) =>
  `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, '0')}`;
