(function () {
  const localKey = 'snowflakes_auth_token';
  const cookieName = 'snowflakes_auth_token';
  const playerNameKey = 'snowflakes_player_name';

  function getCookie(name) {
    const prefix = name + '=';
    return document.cookie
      .split(';')
      .map((value) => value.trim())
      .find((value) => value.startsWith(prefix))
      ?.slice(prefix.length) || '';
  }

  function randomHex() {
    const bytes = new Uint8Array(16);
    crypto.getRandomValues(bytes);
    return Array.from(bytes, (value) => value.toString(16).padStart(2, '0')).join('');
  }

  const localValue = localStorage.getItem(localKey) || randomHex();
  if (!localStorage.getItem(localKey)) {
    localStorage.setItem(localKey, localValue);
  }
  if (getCookie(cookieName) !== localValue) {
    document.cookie = `${cookieName}=${localValue}; path=/; max-age=${60 * 60 * 24 * 365 * 3}; samesite=lax`;
  }

  function persistPlayerName(value) {
    const name = String(value || '').trim();
    if (!name) return;
    localStorage.setItem(playerNameKey, name);
  }

  function hydratePlayerNames(scope) {
    const savedName = (localStorage.getItem(playerNameKey) || '').trim();
    if (!savedName) return;
    for (const input of (scope || document).querySelectorAll('input[name="name"]')) {
      if (!input.value.trim()) {
        input.value = savedName;
      }
    }
  }

  function normalizeRoomCodes(scope) {
    for (const input of (scope || document).querySelectorAll('input[name="code"]')) {
      input.value = input.value.toUpperCase().replace(/\s+/g, '');
    }
  }

  function trimClueInputs(form) {
    if (!(form instanceof HTMLFormElement)) return;
    if (!form.action.includes('/actions/clue')) return;
    for (const input of form.querySelectorAll('input[name^="clue_"]')) {
      if (input instanceof HTMLInputElement) {
        input.value = input.value.trim();
      }
    }
  }

  function fallbackCopyText(text) {
    const textarea = document.createElement('textarea');
    textarea.value = text;
    textarea.setAttribute('readonly', '');
    textarea.style.position = 'fixed';
    textarea.style.top = '0';
    textarea.style.left = '-9999px';
    document.body.appendChild(textarea);
    textarea.focus();
    textarea.select();
    textarea.setSelectionRange(0, textarea.value.length);
    try {
      return !!document.execCommand && document.execCommand('copy');
    } catch {
      return false;
    } finally {
      textarea.remove();
    }
  }

  async function copyText(text) {
    if (navigator.clipboard?.writeText && window.isSecureContext) {
      try {
        await navigator.clipboard.writeText(text);
        return true;
      } catch {
        // fall through to the legacy path below
      }
    }
    return fallbackCopyText(text);
  }

  hydratePlayerNames(document);
  normalizeRoomCodes(document);

  document.addEventListener('input', (event) => {
    const input = event.target;
    if (!(input instanceof HTMLInputElement)) return;
    if (input.name === 'name') {
      persistPlayerName(input.value);
      return;
    }
    if (input.name === 'code') {
      input.value = input.value.toUpperCase().replace(/\s+/g, '');
    }
  });

  document.addEventListener('click', async (event) => {
    const button = event.target.closest('button[data-copy-text]');
    if (!(button instanceof HTMLButtonElement)) return;
    event.preventDefault();
    const text = button.dataset.copyText || '';
    if (!text) return;

    const originalText = button.dataset.copyOriginalText || button.textContent || 'Copy';
    button.dataset.copyOriginalText = originalText;
    if (await copyText(text)) {
      button.textContent = 'Copied!';
      window.setTimeout(() => {
        if (button.isConnected) button.textContent = originalText;
      }, 1500);
      return;
    }
    window.prompt('Copy this room link:', text);
  });

  document.addEventListener('submit', async (event) => {
    const form = event.target;
    if (!(form instanceof HTMLFormElement)) return;

    persistPlayerName(new FormData(form).get('name'));
    normalizeRoomCodes(form);
    trimClueInputs(form);

    if (form.dataset.ajax !== 'true') return;
    event.preventDefault();

    const response = await fetch(form.action, {
      method: form.method || 'POST',
      body: new FormData(form),
      headers: { 'X-Requested-With': 'fetch' },
      credentials: 'same-origin'
    });

    if (response.redirected) {
      window.location.href = response.url;
      return;
    }
    if (!response.ok) {
      alert(await response.text());
      return;
    }
    if (window.snowflakesRefreshRoom) {
      window.snowflakesRefreshRoom();
    }
  });

  const code = document.body.dataset.roomCode;
  if (!code) return;

  const root = document.getElementById('room-root');
  if (!root) return;

  function captureOpenDetails(scope) {
    return Array.from((scope || document).querySelectorAll('details[data-preserve-open]'))
      .filter((detail) => detail.open)
      .map((detail) => detail.dataset.preserveOpen)
      .filter(Boolean);
  }

  function restoreOpenDetails(scope, keys) {
    const keepOpen = new Set(keys || []);
    for (const detail of (scope || document).querySelectorAll('details[data-preserve-open]')) {
      detail.open = keepOpen.has(detail.dataset.preserveOpen);
    }
  }

  let inflight = false;
  window.snowflakesRefreshRoom = async function () {
    if (inflight) return;
    inflight = true;
    root.setAttribute('aria-busy', 'true');

    try {
      const openDetails = captureOpenDetails(root);
      const response = await fetch(`/rooms/${code}/fragment`, { credentials: 'same-origin' });
      if (!response.ok) return;
      root.innerHTML = await response.text();
      hydratePlayerNames(root);
      normalizeRoomCodes(root);
      restoreOpenDetails(root, openDetails);
    } finally {
      root.removeAttribute('aria-busy');
      inflight = false;
    }
  };

  const events = new EventSource(`/rooms/${code}/events`);
  events.addEventListener('refresh', () => window.snowflakesRefreshRoom());
})();
