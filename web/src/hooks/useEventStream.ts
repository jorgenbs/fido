import { useEffect, useRef } from 'react';
import { subscribeEvents, type SSEEvent } from '../api/client';

export function useEventStream(onEvent: (event: SSEEvent) => void) {
  const callbackRef = useRef(onEvent);
  callbackRef.current = onEvent;

  useEffect(() => {
    const es = subscribeEvents((evt) => callbackRef.current(evt));
    return () => es.close();
  }, []);
}
