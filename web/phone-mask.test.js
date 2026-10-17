// node --test web/phone-mask.test.js   (make test-js)
const assert = require('node:assert/strict');
const { readFileSync } = require('node:fs');
const { join } = require('node:path');
const test = require('node:test');
const vm = require('node:vm');

function loadPhoneMask() {
  const context = {};
  vm.createContext(context);
  vm.runInContext(readFileSync(join(__dirname, 'static', 'phone-mask.js'), 'utf8'), context);
  return context.QPhone;
}

test('форматирует казахстанский номер для показа', () => {
  const phone = loadPhoneMask();
  for (const input of ['7024025282', '+77024025282', '77024025282', '87024025282', '+7 702 402 52 82', '+7 (702) 402-52-82']) {
    assert.equal(phone.format(input), '+7 702 402 52 82', input);
  }
});

test('форматирует по мере ввода', () => {
  const phone = loadPhoneMask();
  assert.equal(phone.format('+7 '), '');
  assert.equal(phone.format('+7 70'), '+7 70');
  assert.equal(phone.format('+7 7024'), '+7 702 4');
  assert.equal(phone.format('+7 702 402 5'), '+7 702 402 5');
  assert.equal(phone.format('+7 702 402 52 82 999'), '+7 702 402 52 82');
});

test('нормализует в формат хранения 77024025282', () => {
  const phone = loadPhoneMask();
  assert.equal(phone.normalize('+7 702 402 52 82'), '77024025282');
  assert.equal(phone.normalize('87024025282'), '77024025282');
  assert.equal(phone.normalize('7024025282'), '77024025282');
  assert.equal(phone.normalize(''), '');
});

// Имитация набора по одной цифре: так же, как setupInput переформатирует поле на каждом input.
function typeInto(phone, start, keys) {
  let value = start;
  for (const k of keys) value = phone.format(value + k);
  return value;
}

test('набор с привычной восьмёрки после подставленного +7', () => {
  const phone = loadPhoneMask();
  assert.equal(typeInto(phone, '+7 ', '87024025282'), '+7 702 402 52 82');
  assert.equal(typeInto(phone, '+7 ', '7024025282'), '+7 702 402 52 82');
  assert.equal(typeInto(phone, '', '87024025282'), '+7 702 402 52 82');
  assert.equal(phone.normalize('+7 8 702 402 52 82'), '77024025282');
});
