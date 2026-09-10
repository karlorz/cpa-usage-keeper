// @vitest-environment happy-dom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { expect, it, vi } from 'vitest';
import { Modal } from '../Modal';
import { Select } from '../Select';

globalThis.IS_REACT_ACT_ENVIRONMENT = true;
vi.mock('react-i18next', () => ({ useTranslation: () => ({ t: (key: string) => key }) }));

it.each([false, true])('does not interrupt pointer scrolling with automatic option alignment (searchable: %s)', async (searchable) => {
  const container = document.createElement('div');
  document.body.append(container);
  const root = createRoot(container);
  try {
    await act(async () => root.render(<Select
      value="0" options={Array.from({ length: 12 }, (_, index) => ({ value: String(index), label: `Model ${index}` }))}
      onChange={vi.fn()} search={searchable ? { placeholder: 'Model', noResultsText: 'No models' } : undefined}
    />));
    const trigger = container.querySelector<HTMLInputElement | HTMLButtonElement>(searchable ? 'input' : 'button')!;
    await act(async () => trigger.click());
    const options = document.querySelectorAll<HTMLButtonElement>('[role="option"]');
    const pointerScroll = vi.spyOn(options[1], 'scrollIntoView');
    const keyboardScroll = vi.spyOn(options[2], 'scrollIntoView');
    await act(async () => options[1].dispatchEvent(new MouseEvent('mouseover', { bubbles: true })));
    expect(pointerScroll).not.toHaveBeenCalled();
    await act(async () => trigger.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true })));
    expect(keyboardScroll).toHaveBeenCalledWith({ block: 'nearest' });
  } finally {
    await act(async () => root.unmount());
    container.remove();
    vi.restoreAllMocks();
  }
});

it('closes the nested list before the settings dialog on Escape', async () => {
  const container = document.createElement('div');
  document.body.append(container);
  const root = createRoot(container);
  const close = vi.fn();
  try {
    await act(async () => root.render(
      <Modal open title="Schedule" onClose={close}>
        <Select value="mon" options={[{ value: 'mon', label: 'Monday' }]} onChange={vi.fn()} ariaLabel="Weekday" />
      </Modal>
    ));
    const trigger = document.querySelector<HTMLButtonElement>('[aria-label="Weekday"]')!;
    await act(async () => trigger.click());
    expect(document.querySelector('[role="listbox"]')).not.toBeNull();
    await act(async () => trigger.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })));
    expect(document.querySelector('[role="listbox"]')).toBeNull();
    expect(close).not.toHaveBeenCalled();
    expect(document.activeElement).toBe(trigger);
    await act(async () => trigger.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })));
    expect(close).toHaveBeenCalledOnce();
  } finally {
    await act(async () => root.unmount());
    container.remove();
  }
});

it.each([{ x: 120, expected: 120 }, { x: 980, expected: 836 }])(
  'aligns a wider menu with its trigger and keeps it inside the viewport at $x',
  async ({ x, expected }) => {
    const container = document.createElement('div');
    document.body.append(container);
    const root = createRoot(container);
    vi.stubGlobal('innerWidth', 1024);
    try {
      await act(async () => root.render(<Select value="a" options={[{ value: 'a', label: 'API Key' }]} onChange={vi.fn()} dropdownMinWidth={180} />));
      const trigger = container.querySelector('button')!;
      vi.spyOn(trigger.parentElement!, 'getBoundingClientRect').mockReturnValue(new DOMRect(x, 50, 44, 36));
      await act(async () => trigger.click());
      const menu = document.querySelector<HTMLElement>('[role="listbox"]')!;
      expect(menu.style.width).toBe('180px');
      expect(menu.style.left).toBe(`${expected}px`);
    } finally {
      await act(async () => root.unmount());
      container.remove();
      vi.unstubAllGlobals();
      vi.restoreAllMocks();
    }
  }
);

it('follows its anchor when layout moves it without resizing the trigger or viewport', async () => {
  const container = document.createElement('div');
  document.body.append(container);
  const root = createRoot(container);
  try {
    await act(async () => root.render(<Select value="a" options={[{ value: 'a', label: 'API Key' }]} onChange={vi.fn()} />));
    const trigger = container.querySelector('button')!;
    const bounds = vi.spyOn(trigger.parentElement!, 'getBoundingClientRect').mockReturnValue(new DOMRect(600, 80, 180, 44));
    await act(async () => trigger.click());
    const menu = document.querySelector<HTMLElement>('[role="listbox"]')!;
    expect(menu.style.left).toBe('600px');
    bounds.mockReturnValue(new DOMRect(38, 140, 180, 44));
    await act(async () => { await new Promise(requestAnimationFrame); });
    expect(menu.style.left).toBe('38px');
    expect(menu.style.top).toBe('192px');
  } finally {
    await act(async () => root.unmount());
    container.remove();
    vi.restoreAllMocks();
  }
});
