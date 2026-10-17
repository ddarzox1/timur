// Немного JS там, где без него нельзя: подпись, геолокация, «дочитал до конца», ошибки форм.

document.body.addEventListener('htmx:responseError', (e) => {
  toast(e.detail.xhr.responseText || 'Ошибка');
});
document.body.addEventListener('htmx:sendError', () => toast('Нет связи с сервером'));

function toast(text) {
  const t = document.getElementById('toast');
  t.innerHTML = '';
  const d = document.createElement('div');
  d.className = 'error';
  d.textContent = text;
  t.append(d);
  setTimeout(() => d.remove(), 5000);
}

htmx.onLoad((root) => {
  // Подписи колонок для таблиц-карточек на телефоне
  root.querySelectorAll('table.rtable').forEach((t) => {
    const heads = [...t.querySelectorAll('thead th')].map((th) => th.textContent.trim());
    t.querySelectorAll('tbody tr').forEach((tr) => [...tr.children].forEach((td, i) => { if (heads[i] && !td.hasAttribute('colspan')) td.dataset.label = heads[i]; }));
  });
  // Кнопка меню доступна с клавиатуры
  root.querySelectorAll('.burger').forEach((b) => b.addEventListener('keydown', (e) => {
    if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); b.click(); }
  }));

  // Подпись пальцем: <canvas data-pad="имя-скрытого-поля">
  root.querySelectorAll('canvas[data-pad]').forEach((c) => {
    const input = document.querySelector(`input[name="${c.dataset.pad}"]`);
    const ctx = c.getContext('2d');
    const fit = () => {
      const r = c.getBoundingClientRect();
      c.width = r.width * devicePixelRatio;
      c.height = r.height * devicePixelRatio;
      ctx.scale(devicePixelRatio, devicePixelRatio);
      ctx.lineWidth = 2.5; ctx.lineCap = 'round'; ctx.strokeStyle = '#0f2a4a';
    };
    fit();
    let drawing = false, dirty = false;
    const pos = (e) => { const r = c.getBoundingClientRect(); return [e.clientX - r.left, e.clientY - r.top]; };
    c.addEventListener('pointerdown', (e) => { drawing = true; c.setPointerCapture(e.pointerId); ctx.beginPath(); ctx.moveTo(...pos(e)); });
    c.addEventListener('pointermove', (e) => { if (!drawing) return; ctx.lineTo(...pos(e)); ctx.stroke(); dirty = true; });
    c.addEventListener('pointerup', () => { drawing = false; if (dirty) { input.value = c.toDataURL('image/png'); input.dispatchEvent(new Event('change', { bubbles: true })); } });
    const clear = document.querySelector(`[data-clear="${c.dataset.pad}"]`);
    clear?.addEventListener('click', () => { ctx.clearRect(0, 0, c.width, c.height); input.value = ''; dirty = false; input.dispatchEvent(new Event('change', { bubbles: true })); });
  });

  // Геолокация: [data-geo] с data-lat/lng/radius объекта заполняет lat/lng формы
  root.querySelectorAll('[data-geo]').forEach((el) => {
    const form = el.closest('form');
    if (!navigator.geolocation) { el.textContent = el.dataset.na; return; }
    navigator.geolocation.getCurrentPosition((p) => {
      form.querySelector('[name=lat]').value = p.coords.latitude;
      form.querySelector('[name=lng]').value = p.coords.longitude;
      const lat = parseFloat(el.dataset.lat), lng = parseFloat(el.dataset.lng);
      if (isNaN(lat)) { el.textContent = '📍 ' + p.coords.latitude.toFixed(4) + ', ' + p.coords.longitude.toFixed(4); return; }
      const d = dist(p.coords.latitude, p.coords.longitude, lat, lng);
      const ok = d <= parseFloat(el.dataset.radius);
      el.className = 'badge ' + (ok ? 'ok' : 'warn');
      el.textContent = ok ? el.dataset.ok : el.dataset.out;
      form.dispatchEvent(new Event('change'));
    }, () => { el.className = 'badge warn'; el.textContent = el.dataset.na; }, { enableHighAccuracy: true, timeout: 15000 });
  });

  // Кнопка подписи активна, когда документ дочитан, галочка стоит и подпись есть
  root.querySelectorAll('form[data-sign]').forEach((form) => {
    const doc = form.querySelector('.doc');
    const btn = form.querySelector('[type=submit]');
    const agree = form.querySelector('[name=agree]');
    let read = !doc || doc.scrollHeight <= doc.clientHeight + 4;
    const update = () => {
      agree.disabled = !read;
      btn.disabled = !(read && agree.checked && form.querySelector('[name=signature]').value &&
        (!form.dataset.needGeo || form.querySelector('[name=lat]').value));
    };
    doc?.addEventListener('scroll', () => { if (doc.scrollTop + doc.clientHeight >= doc.scrollHeight - 8) { read = true; update(); } });
    form.addEventListener('change', update);
    update();
  });
});

function dist(a1, o1, a2, o2) {
  const R = 6371000, r = Math.PI / 180;
  const h = Math.sin((a2 - a1) * r / 2) ** 2 + Math.cos(a1 * r) * Math.cos(a2 * r) * Math.sin((o2 - o1) * r / 2) ** 2;
  return 2 * R * Math.asin(Math.sqrt(h));
}
