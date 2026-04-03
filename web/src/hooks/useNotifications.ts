import { useState, useCallback } from 'react';

type Permission = 'default' | 'granted' | 'denied';

export function useNotifications() {
  const [permission, setPermission] = useState<Permission>(
    typeof Notification !== 'undefined' ? Notification.permission as Permission : 'denied'
  );

  const requestPermission = useCallback(async () => {
    if (typeof Notification === 'undefined') return;
    const result = await Notification.requestPermission();
    setPermission(result as Permission);
  }, []);

  const notify = useCallback((title: string, options?: NotificationOptions & { onClick?: () => void }) => {
    if (permission !== 'granted') return;
    if (typeof Notification === 'undefined') return;
    const { onClick, ...notifOptions } = options ?? {};
    const n = new Notification(title, { icon: '/favicon.svg', ...notifOptions });
    n.onclick = () => {
      window.focus();
      onClick?.();
      n.close();
    };
  }, [permission]);

  return { permission, requestPermission, notify };
}
