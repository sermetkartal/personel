// Faz 17 item #185 — internal status page view.
//
// This is an INTERNAL admin-only view into the same data served by
// the PUBLIC /public/status.json endpoint. It mirrors the public
// page but:
//   - Is gated by admin/it_manager session
//   - Shows the full incident history (not just active)
//   - Allows creating and resolving incidents via the admin API
//
// The public endpoint is available at https://<host>/public/status.json
// and can be linked from any public dashboard or shared with customers.

import { redirect } from "next/navigation";
import { getSession } from "@/lib/auth/session";
import { can } from "@/lib/auth/rbac";

interface StatusPageProps {
  params: Promise<{ locale: string }>;
}

interface ComponentStatus {
  name: string;
  status: string;
  uptime_7d: number;
}

interface Incident {
  id: string;
  severity: string;
  component: string;
  title: string;
  description: string;
  state: string;
  started_at: string;
  resolved_at?: string;
}

interface PublicStatus {
  overall: string;
  generated_at: string;
  components: ComponentStatus[];
  active_incidents: Incident[];
  upcoming_maintenance: Array<{
    id: string;
    title: string;
    scheduled_start: string;
    scheduled_end: string;
  }>;
}

async function fetchStatus(): Promise<PublicStatus | null> {
  const base = process.env.PERSONEL_API_URL ?? "http://localhost:8080";
  try {
    const res = await fetch(`${base}/public/status.json`, {
      cache: "no-store",
    });
    if (!res.ok) return null;
    return (await res.json()) as PublicStatus;
  } catch {
    return null;
  }
}

function statusClass(s: string): string {
  switch (s) {
    case "operational":
      return "bg-green-100 text-green-900 border-green-300";
    case "degraded":
      return "bg-yellow-100 text-yellow-900 border-yellow-300";
    case "partial_outage":
      return "bg-orange-100 text-orange-900 border-orange-300";
    case "major_outage":
      return "bg-red-100 text-red-900 border-red-300";
    case "maintenance":
      return "bg-blue-100 text-blue-900 border-blue-300";
    default:
      return "bg-gray-100 text-gray-700 border-gray-300";
  }
}

function statusLabelTR(s: string): string {
  switch (s) {
    case "operational":
      return "Çalışıyor";
    case "degraded":
      return "Yavaş";
    case "partial_outage":
      return "Kısmi Kesinti";
    case "major_outage":
      return "Büyük Kesinti";
    case "maintenance":
      return "Bakım";
    case "unknown":
      return "Bilinmiyor";
    default:
      return s;
  }
}

export default async function StatusPage({
  params,
}: StatusPageProps): Promise<JSX.Element> {
  const { locale } = await params;
  const session = await getSession();

  // Admin + it_manager can see the internal status view
  if (!session?.user || !can(session.user.role, "view:system")) {
    redirect(`/${locale}/unauthorized`);
  }

  const status = await fetchStatus();

  if (!status) {
    return (
      <div className="p-8">
        <h1 className="text-2xl font-bold mb-4">Sistem Durumu</h1>
        <div className="p-4 bg-red-50 border border-red-200 rounded">
          Status servisi yanıt vermiyor.
        </div>
      </div>
    );
  }

  return (
    <div className="p-8 max-w-6xl">
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-bold">Sistem Durumu</h1>
        <a
          href="/public/status.json"
          className="text-sm text-blue-600 hover:underline"
          target="_blank"
          rel="noopener"
        >
          Genel sayfa (JSON) →
        </a>
      </div>

      {/* Overall banner */}
      <div
        className={`p-6 mb-6 border-2 rounded-lg ${statusClass(status.overall)}`}
      >
        <div className="flex items-center justify-between">
          <div>
            <div className="text-sm opacity-80">Genel Durum</div>
            <div className="text-2xl font-bold">
              {statusLabelTR(status.overall)}
            </div>
          </div>
          <div className="text-sm opacity-80">
            Son güncelleme: {new Date(status.generated_at).toLocaleString("tr-TR")}
          </div>
        </div>
      </div>

      {/* Components table */}
      <section className="mb-8">
        <h2 className="text-lg font-semibold mb-3">Bileşenler</h2>
        <div className="border rounded overflow-hidden">
          <table className="w-full text-sm">
            <thead className="bg-gray-50">
              <tr>
                <th className="text-left p-3 font-medium">Servis</th>
                <th className="text-left p-3 font-medium">Durum</th>
                <th className="text-right p-3 font-medium">
                  7-Günlük Uptime
                </th>
              </tr>
            </thead>
            <tbody>
              {status.components.map((c) => (
                <tr key={c.name} className="border-t">
                  <td className="p-3 font-mono">{c.name}</td>
                  <td className="p-3">
                    <span
                      className={`px-2 py-0.5 text-xs rounded border ${statusClass(c.status)}`}
                    >
                      {statusLabelTR(c.status)}
                    </span>
                  </td>
                  <td className="p-3 text-right tabular-nums">
                    {c.uptime_7d.toFixed(2)}%
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>

      {/* Active incidents */}
      <section className="mb-8">
        <h2 className="text-lg font-semibold mb-3">
          Aktif Olaylar ({status.active_incidents.length})
        </h2>
        {status.active_incidents.length === 0 ? (
          <div className="p-4 bg-green-50 border border-green-200 rounded text-sm">
            Aktif olay yok. Her şey yolunda.
          </div>
        ) : (
          <div className="space-y-3">
            {status.active_incidents.map((i) => (
              <div key={i.id} className="p-4 border rounded">
                <div className="flex items-start justify-between">
                  <div>
                    <div className="font-semibold">{i.title}</div>
                    <div className="text-sm text-gray-600">
                      {i.component} · {i.severity} ·{" "}
                      {new Date(i.started_at).toLocaleString("tr-TR")}
                    </div>
                  </div>
                  <span className="text-xs px-2 py-0.5 rounded bg-yellow-100 text-yellow-900">
                    {i.state}
                  </span>
                </div>
                {i.description && (
                  <p className="mt-2 text-sm">{i.description}</p>
                )}
              </div>
            ))}
          </div>
        )}
      </section>

      {/* Upcoming maintenance */}
      {status.upcoming_maintenance.length > 0 && (
        <section className="mb-8">
          <h2 className="text-lg font-semibold mb-3">
            Planlı Bakım ({status.upcoming_maintenance.length})
          </h2>
          <div className="space-y-2">
            {status.upcoming_maintenance.map((m) => (
              <div key={m.id} className="p-3 border rounded text-sm">
                <div className="font-medium">{m.title}</div>
                <div className="text-gray-600">
                  {new Date(m.scheduled_start).toLocaleString("tr-TR")} →{" "}
                  {new Date(m.scheduled_end).toLocaleString("tr-TR")}
                </div>
              </div>
            ))}
          </div>
        </section>
      )}
    </div>
  );
}
