import { useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { formatUsd, formatPoePoints } from '@/utils/usage';
import type { SpendDashboardRow } from '@/lib/types';
import { LoadingSpinner } from '@/components/ui/LoadingSpinner';
import styles from './SpendDashboardCard.module.scss';

interface SpendDashboardCardProps {
  rows: SpendDashboardRow[];
  loading?: boolean;
}

export function SpendDashboardCard({ rows, loading = false }: SpendDashboardCardProps) {
  const { t } = useTranslation();

  const totals = useMemo(() => {
    let totalUSD = 0;
    let totalPoints = 0;
    for (const r of rows) {
      totalUSD += r.usd_spent || 0;
      totalPoints += r.points_spent || 0;
    }
    return { totalUSD, totalPoints };
  }, [rows]);

  return (
    <div className={styles.spendDashboardCard}>
      <div className={styles.header}>
        <div className={styles.titleGroup}>
          <h3 className={styles.title}>{t('usage_stats.spend_dashboard_title')}</h3>
          <p className={styles.subtitle}>{t('usage_stats.spend_dashboard_subtitle')}</p>
        </div>
        <div className={styles.summaryBadges}>
          <span className={`${styles.badge} ${styles.badgeUSD}`}>
            <span>{t('usage_stats.spend_total_usd')}:</span>
            <strong>{formatUsd(totals.totalUSD)}</strong>
          </span>
          <span className={`${styles.badge} ${styles.badgePoints}`}>
            <span>{t('usage_stats.spend_total_points')}:</span>
            <strong>{formatPoePoints(totals.totalPoints)}</strong>
          </span>
        </div>
      </div>

      {loading ? (
        <div className={styles.loadingWrapper}>
          <LoadingSpinner />
        </div>
      ) : rows.length === 0 ? (
        <div className={styles.emptyState}>
          {t('usage_stats.spend_empty')}
        </div>
      ) : (
        <div className={styles.tableWrapper}>
          <table className={styles.table}>
            <thead>
              <tr>
                <th>{t('usage_stats.spend_col_date')}</th>
                <th>{t('usage_stats.spend_col_identity')}</th>
                <th>{t('usage_stats.spend_col_model')}</th>
                <th className={styles.numericCell}>{t('usage_stats.spend_col_usd')}</th>
                <th className={styles.numericCell}>{t('usage_stats.spend_col_points')}</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((row, idx) => (
                <tr key={`${row.date}-${row.auth_index}-${row.model}-${idx}`}>
                  <td>{row.date}</td>
                  <td className={styles.authCell}>{row.auth_index || '-'}</td>
                  <td className={styles.modelCell}>{row.model || '-'}</td>
                  <td className={`${styles.numericCell} ${styles.usdValue}`}>
                    {row.usd_spent > 0 ? formatUsd(row.usd_spent) : '$0.00'}
                  </td>
                  <td className={`${styles.numericCell} ${styles.pointsValue}`}>
                    {row.points_spent > 0 ? formatPoePoints(row.points_spent) : '0'}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
