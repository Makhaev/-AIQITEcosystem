import { useEffect, useState } from 'react';

// Carry a one-time save confirmation to the newly created card.
export function navigateWithNotice(hash: string, notice: string) {
  window.location.hash = hash;
  window.history.replaceState({ ...window.history.state, aiqNotice: notice }, '', window.location.href);
}

export function useNavigationNotice() {
  const [notice, setNotice] = useState<string>(() => window.history.state?.aiqNotice || '');
  useEffect(() => {
    if (!window.history.state?.aiqNotice) return;
    const { aiqNotice: _, ...rest } = window.history.state;
    window.history.replaceState(rest, '', window.location.href);
  }, []);
  return [notice, setNotice] as const;
}
