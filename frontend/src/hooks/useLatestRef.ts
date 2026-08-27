import { useEffect, useRef } from "react";

/**
 * Keeps a ref pointing at the most recent value without writing to it during
 * render. Long-lived subscriptions (SSE listeners, document event handlers,
 * timers) need the current callback or value without re-subscribing whenever
 * it changes, and reading a ref inside the handler achieves that.
 *
 * The assignment happens in an unkeyed effect rather than in the render body:
 * writing to a ref while rendering is a side effect, so React may discard or
 * replay it under concurrent rendering. Consumers must therefore read
 * `.current` from a handler, effect, or timer — never during render.
 */
export function useLatestRef<T>(value: T) {
  const ref = useRef(value);

  useEffect(() => {
    ref.current = value;
  });

  return ref;
}
