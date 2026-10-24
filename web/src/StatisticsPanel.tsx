import type { Language } from './i18n';
import { bytes } from './protocol';
import { transferText } from './transfer-i18n';
import type { Statistics } from './types';
export function StatisticsPanel({ statistics, language }: { statistics?: Statistics; language: Language }) {
  const t = transferText(language);
  if (!statistics) return <section className="feature-section"><h2>{t.statistics}</h2><p className="mobile-note">{t.noStatistics}</p></section>;
  const values = [[t.sent, bytes(statistics.sentBytes, language)], [t.received, bytes(statistics.receivedBytes, language)], [t.completed, statistics.completedTransfers], [t.failedCount, statistics.failedTransfers], [t.cancelledCount, statistics.cancelledTransfers], [t.active, statistics.activeTransfers]];
  return <section className="feature-section"><div className="section-heading"><h2>{t.statistics}</h2></div><h3 className="statistics-heading">{t.verified}</h3><p className="feature-intro">{t.confirmedHelp}</p><dl className="statistics-grid">{values.map(([label, value]) => <div key={label}><dt>{label}</dt><dd>{value}</dd></div>)}</dl>
    <h3 className="statistics-heading">{t.relayStatistics}</h3><dl className="statistics-grid relay-statistics"><div><dt>{t.relayBytes}</dt><dd>{bytes(statistics.relayedBytes, language)}</dd></div><div><dt>{t.relayDeliveries}</dt><dd>{statistics.relayDeliveries}</dd></div></dl><p className="feature-intro">{t.deliveredHelp}</p>
    <h3 className="statistics-heading">{t.daily}</h3><div className="table-scroll"><table className="statistics-table"><thead><tr><th>{t.date}</th><th>{t.sent}</th><th>{t.received}</th><th>{t.relay}</th><th>{t.completed}</th></tr></thead><tbody>{(statistics.days || []).map(day => <tr key={day.date}><td>{day.date}</td><td>{bytes(day.sentBytes, language)}</td><td>{bytes(day.receivedBytes, language)}</td><td>{bytes(day.relayedBytes, language)}</td><td>{day.completedTransfers}</td></tr>)}</tbody></table></div>
  </section>;
}
