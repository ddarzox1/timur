// Маска казахстанского номера: +7 702 402 52 82. Портировано из birtap.kz (static/js/phone-mask.js),
// исправлен ввод с восьмёрки после подставленного «+7 ».
// Хранится номер на сервере как 77024025282 — его приводит domain.NormalizePhone.
(function(root) {
  'use strict';

  // nationalDigits — 10 цифр после кода страны: убирает +7, ведущую 8 или ведущую 7 у 11-значного номера.
  function nationalDigits(value) {
    var text = String(value || '').trim();
    var digits = text.replace(/\D/g, '');
    if (!digits) return '';

    if (text.indexOf('+7') === 0) {
      digits = digits.slice(1);
    } else if (digits[0] === '8') {
      digits = digits.slice(1);
    } else if (digits.length > 10 && digits[0] === '7') {
      digits = digits.slice(1);
    }
    // Номер в Казахстане после +7 не начинается с 8: это набранный по привычке префикс «8 702…»
    // поверх подставленного «+7 ». Без этого +7 8702… превращался в чужой номер 78702….
    if (digits[0] === '8') {
      digits = digits.slice(1);
    }

    return digits.slice(0, 10);
  }

  // format — для показа и по мере ввода: +7 702 402 52 82.
  function format(value) {
    var digits = nationalDigits(value);
    if (!digits) return '';

    var out = '+7';
    if (digits.length > 0) out += ' ' + digits.slice(0, 3);
    if (digits.length > 3) out += ' ' + digits.slice(3, 6);
    if (digits.length > 6) out += ' ' + digits.slice(6, 8);
    if (digits.length > 8) out += ' ' + digits.slice(8, 10);
    return out;
  }

  // normalize — формат хранения: 77024025282.
  function normalize(value) {
    var digits = nationalDigits(value);
    return digits ? '7' + digits : '';
  }

  function setupInput(input) {
    if (!input || input.dataset.phoneMask) return;
    input.dataset.phoneMask = '1';
    input.setAttribute('inputmode', 'tel');
    input.setAttribute('autocomplete', 'tel');
    input.addEventListener('input', function() {
      input.value = format(input.value);
    });
    input.addEventListener('focus', function() {
      if (!input.value) input.value = '+7 ';
    });
    input.addEventListener('blur', function() {
      input.value = format(input.value);
    });
    if (input.value) input.value = format(input.value);
  }

  root.QPhone = {
    nationalDigits: nationalDigits,
    format: format,
    normalize: normalize,
    setupInput: setupInput,
  };

  // Автоинициализация при загрузке страницы: раньше поле #phone ждало события htmx:load
  // из app.js, которое на обычной (не htmx-boosted) загрузке срабатывает не всегда —
  // из-за этого маска на /login иногда не применялась вообще. Теперь скрипт сам
  // проставляет маску на все input[type=tel], как только DOM готов; app.js
  // по-прежнему довешивает её на поля, подгруженные через htmx позже.
  if (typeof document !== 'undefined') {
    var initAll = function() {
      document.querySelectorAll('input[type=tel]').forEach(setupInput);
    };
    if (document.readyState === 'loading') {
      document.addEventListener('DOMContentLoaded', initAll);
    } else {
      initAll();
    }
  }

  if (typeof module !== 'undefined' && module.exports) {
    module.exports = root.QPhone;
  }
})(typeof window !== 'undefined' ? window : globalThis);
