import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';

const styles = readFileSync(new URL('../cpamcEmbed.css', import.meta.url), 'utf8');

describe('CPAMC embed styles', () => {
  it('scopes every selector, including media rules, to the embed root', () => {
    const selectors = [...styles.matchAll(/([^{}]+)\{/g)]
      .map((match) => match[1].trim())
      .filter((header) => !header.startsWith('@'))
      .flatMap((header) => header.split(','));
    expect(selectors.length).toBeGreaterThan(0);
    for (const selector of selectors) {
      expect(selector.trim()).toMatch(/^\.app-frame\[data-embed='cpamc'\](?:\s|$)/);
    }
  });
});
