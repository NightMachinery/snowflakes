(function () {
  const localKey = 'snowflakes_auth_token';
  const cookieName = 'snowflakes_auth_token';

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

  document.addEventListener('submit', async (event) => {
    const form = event.target;
    if (!(form instanceof HTMLFormElement)) return;
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
      }
    } finally {
      inflight = false;
    }
  };

  const es = new EventSource(`/rooms/${code}/events`);
  es.addEventListener('refresh', () => window.snowflakesRefreshRoom());
})();
