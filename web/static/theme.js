(function () {
  'use strict';

  const storageKey = 'qauipsiz-theme';
  const darkQuery = window.matchMedia('(prefers-color-scheme: dark)');

  function savedTheme() {
    try {
      const value = localStorage.getItem(storageKey);
      return value === 'dark' || value === 'light' ? value : null;
    } catch (_) {
      return null;
    }
  }

  function currentTheme() {
    return document.documentElement.dataset.theme || (darkQuery.matches ? 'dark' : 'light');
  }

  function copy() {
    const kk = document.documentElement.lang === 'kk';
    const dark = currentTheme() === 'dark';
    return dark
      ? { label: kk ? 'Ашық' : 'Светлая', title: kk ? 'Ашық тақырыпты қосу' : 'Включить светлую тему' }
      : { label: kk ? 'Қараңғы' : 'Тёмная', title: kk ? 'Қараңғы тақырыпты қосу' : 'Включить тёмную тему' };
  }

  function syncControls(root) {
    const text = copy();
    const dark = currentTheme() === 'dark';
    const scope = root || document;
    scope.querySelectorAll('[data-theme-source-dark]').forEach((source) => {
      source.media = dark ? 'all' : 'not all';
    });
    scope.querySelectorAll('[data-theme-toggle]').forEach((button) => {
      button.dataset.themeCurrent = dark ? 'dark' : 'light';
      button.setAttribute('aria-pressed', String(dark));
      button.setAttribute('aria-label', text.title);
      button.title = text.title;
      const label = button.querySelector('[data-theme-label]');
      if (label) label.textContent = text.label;
      if (!button.dataset.themeReady) {
        button.dataset.themeReady = 'true';
        button.addEventListener('click', toggleTheme);
      }
    });
  }

  function applyTheme(theme, persist) {
    document.documentElement.dataset.theme = theme;
    document.documentElement.style.colorScheme = theme;
    const meta = document.querySelector('meta[name="theme-color"]');
    if (meta) meta.content = theme === 'dark' ? '#202127' : '#F3F4F6';
    if (persist) {
      try { localStorage.setItem(storageKey, theme); } catch (_) { /* Storage may be disabled. */ }
    }
    syncControls();
  }

  function toggleTheme() {
    applyTheme(currentTheme() === 'dark' ? 'light' : 'dark', true);
  }

  applyTheme(savedTheme() || (darkQuery.matches ? 'dark' : 'light'), false);

  document.addEventListener('DOMContentLoaded', () => syncControls());
  document.addEventListener('htmx:afterSwap', (event) => syncControls(event.detail.target));
  darkQuery.addEventListener?.('change', (event) => {
    if (!savedTheme()) applyTheme(event.matches ? 'dark' : 'light', false);
  });
})();
