import { describe, it, expect } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import { SpendDashboardCard } from '../SpendDashboardCard';
import type { SpendDashboardRow } from '@/lib/types';

describe('SpendDashboardCard', () => {
  it('renders empty state when no rows provided', () => {
    const markup = renderToStaticMarkup(<SpendDashboardCard rows={[]} loading={false} />);
    expect(markup).toContain('No spend data for the selected range.');
    expect(markup).toContain('Spend Summary');
  });

  it('renders table with USD and Poe compute points side by side', () => {
    const rows: SpendDashboardRow[] = [
      {
        auth_index: 'auth-1',
        model: 'deepseek-v4-flash',
        date: '2026-08-27',
        usd_spent: 1.23,
        points_spent: 4567,
      },
      {
        auth_index: 'auth-2',
        model: 'claude-3-5-sonnet',
        date: '2026-08-26',
        usd_spent: 5.50,
        points_spent: 0,
      },
    ];

    const markup = renderToStaticMarkup(<SpendDashboardCard rows={rows} loading={false} />);
    expect(markup).toContain('auth-1');
    expect(markup).toContain('deepseek-v4-flash');
    expect(markup).toContain('2026-08-27');
    expect(markup).toContain('$1.23');
    expect(markup).toContain('4,567');
    expect(markup).toContain('auth-2');
    expect(markup).toContain('claude-3-5-sonnet');
    expect(markup).toContain('$5.50');
    expect(markup).toContain('Spend Summary');
    expect(markup).toContain('USD Spent');
    expect(markup).toContain('Points Spent');
  });

  it('calculates and displays totals in header badges', () => {
    const rows: SpendDashboardRow[] = [
      {
        auth_index: 'auth-1',
        model: 'model-a',
        date: '2026-08-27',
        usd_spent: 2.00,
        points_spent: 1000,
      },
      {
        auth_index: 'auth-2',
        model: 'model-b',
        date: '2026-08-27',
        usd_spent: 3.00,
        points_spent: 2000,
      },
    ];

    const markup = renderToStaticMarkup(<SpendDashboardCard rows={rows} loading={false} />);
    expect(markup).toContain('$5.00'); // total USD
    expect(markup).toContain('3,000'); // total points
  });
});
