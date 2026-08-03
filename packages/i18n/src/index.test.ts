import { describe, expect, test } from 'vitest';

import {
  DICTIONARIES,
  TERMINOLOGY,
  createTranslator,
  getLocale,
  setLocale,
} from './index';

describe('i18n', () => {
  test('defaults to Chinese and interpolates variables', () => {
    expect(getLocale()).toBe('zh-CN');
    const { t } = createTranslator();
    expect(t('task.objectiveFor', { target: 'juice-shop.lab' })).toBe('评估 juice-shop.lab');
  });

  test('keeps Chinese and English dictionary keys exactly aligned', () => {
    expect(Object.keys(DICTIONARIES.en).sort()).toEqual(Object.keys(DICTIONARIES['zh-CN']).sort());
    expect(Object.values(DICTIONARIES.en).every(Boolean)).toBe(true);
    expect(Object.values(DICTIONARIES['zh-CN']).every(Boolean)).toBe(true);
  });

  test('switches locale without falling back to untranslated keys', () => {
    setLocale('en');
    const { t } = createTranslator();
    expect(t('approval.allowOnce')).toBe('Allow once');
    setLocale('zh-CN');
    expect(createTranslator().t('approval.allowOnce')).toBe('仅允许一次');
  });

  test('translates the reviewed approval flow in both locales', () => {
    expect(createTranslator('en').t('approval.review')).toBe('Review parameters');
    expect(createTranslator('en').t('approval.confirmAllowOnce')).toBe('Confirm allow once');
    expect(createTranslator('zh-CN').t('approval.review')).toBe('复核参数');
    expect(createTranslator('zh-CN').t('approval.confirmAllowOnce')).toBe('确认仅允许一次');
  });

  test('defines required security terminology in both locales', () => {
    expect(Object.keys(TERMINOLOGY)).toEqual(expect.arrayContaining([
      'Finding', 'Evidence', 'Scope', 'Runtime', 'Agent', 'CVSS',
    ]));
    for (const term of Object.values(TERMINOLOGY)) {
      expect(term['zh-CN']).toBeTruthy();
      expect(term.en).toBeTruthy();
    }
  });

  test('keeps visible English labels bounded for compact layouts', () => {
    expect(Math.max(...Object.values(DICTIONARIES.en).map((label) => label.length))).toBeLessThan(120);
  });
});
