(function () {
  const localKey = 'snowflakes_auth_token';
  const cookieName = 'snowflakes_auth_token';
  const playerNameKey = 'snowflakes_player_name';

  function getCookie(name) {
    const prefix = name + '=';
    return document.cookie.split(';').map(x => x.trim()).find(x => x.startsWith(prefix))?.slice(prefix.length) || '';
  }

  function randomHex() {
    const bytes = new Uint8Array(16);
    crypto.getRandomValues(bytes);
    return Array.from(bytes, b => b.toString(16).padStart(2, '0')).join('');
  }

  const localValue = localStorage.getItem(localKey) || randomHex();
  if (!localStorage.getItem(localKey)) {
    localStorage.setItem(localKey, localValue);
  }
  const cookieValue = getCookie(cookieName);
  if (cookieValue !== localValue) {
    document.cookie = `${cookieName}=${localValue}; path=/; max-age=${60 * 60 * 24 * 365 * 3}; samesite=lax`;
  }

  function persistPlayerName(value) {
    const name = (value || '').trim();
    if (!name) return;
    localStorage.setItem(playerNameKey, name);
  }

  function hydratePlayerNames(root) {
    const savedName = (localStorage.getItem(playerNameKey) || '').trim();
    if (!savedName) return;
    for (const input of (root || document).querySelectorAll('input[name="name"]')) {
      if (!input.value.trim()) {
        input.value = savedName;
      }
    }
  }

  hydratePlayerNames(document);

  document.addEventListener('input', (event) => {
    const input = event.target;
    if (!(input instanceof HTMLInputElement)) return;
    if (input.name !== 'name') return;
    persistPlayerName(input.value);
  });

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
      } catch {}
    }
    return fallbackCopyText(text);
  }

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
    if (form.dataset.ajax !== 'true') return;
    event.preventDefault();
    const response = await fetch(form.action, {
      method: form.method || 'POST',
      body: new FormData(form),
      headers: {'X-Requested-With': 'fetch'},
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

  let inflight = false;
  window.snowflakesRefreshRoom = async function () {
    if (inflight) return;
    inflight = true;
    try {
      const response = await fetch(`/rooms/${code}/fragment`, {credentials: 'same-origin'});
      if (response.ok) {
        root.innerHTML = await response.text();
        hydratePlayerNames(root);
      }
    } finally {
      inflight = false;
    }
  };

  const es = new EventSource(`/rooms/${code}/events`);
  es.addEventListener('refresh', () => window.snowflakesRefreshRoom());
})();
