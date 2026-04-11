/**
 * Turkish string dictionary for the Personel Mobile Admin app.
 * Minimal subset covering the 5 in-scope screens only.
 * Sourced from apps/console/messages/tr.json for parity.
 */

export const tr = {
  common: {
    appName: "Personel Admin",
    loading: "Yükleniyor...",
    saving: "Kaydediliyor...",
    save: "Kaydet",
    cancel: "İptal",
    confirm: "Onayla",
    close: "Kapat",
    back: "Geri",
    retry: "Tekrar Dene",
    refresh: "Yenile",
    yes: "Evet",
    no: "Hayır",
    unknown: "Bilinmiyor",
    status: "Durum",
    actions: "İşlemler",
    details: "Detaylar",
    createdAt: "Oluşturulma Tarihi",
    noResults: "Kayıt bulunamadı.",
    errorGeneric: "Beklenmeyen bir hata oluştu. Lütfen tekrar deneyin.",
    errorNetwork: "Sunucuya bağlanılamıyor. Ağ bağlantınızı kontrol edin.",
    errorUnauthorized: "Bu işlemi gerçekleştirme yetkiniz yok.",
    errorForbidden: "Bu kaynağa erişim reddedildi.",
    errorNotFound: "Aranan kaynak bulunamadı.",
    errorSessionExpired: "Oturumunuz sona erdi.",
  },

  auth: {
    heading: "Personel Yönetici",
    subheading: "Nöbet yönetimi uygulaması",
    loginButton: "Kurumsal Hesapla Giriş Yap",
    loginDescription:
      "Keycloak kurumsal kimlik sağlayıcınız ile güvenli giriş yapın.",
    kvkkNotice:
      "Bu sistem KVKK (6698 sayılı Kanun) kapsamında çalışmaktadır. Giriş yaparak sistem politikalarını kabul etmiş sayılırsınız.",
    redirecting: "Yönlendiriliyor...",
    processing: "Kimlik doğrulanıyor...",
    errorTitle: "Giriş Başarısız",
    exchangeFailed: "Token değişimi başarısız. Lütfen tekrar deneyin.",
    networkError:
      "Sunucuya bağlanılamadı. Ağ bağlantınızı kontrol edin.",
    tryAgain: "Tekrar Giriş Yap",
    logout: "Çıkış Yap",
    sessionExpired: "Oturumunuz sona erdi. Lütfen tekrar giriş yapın.",
  },

  nav: {
    home: "Ana Sayfa",
    liveView: "Canlı İzleme",
    dsr: "Veri Talepleri",
    silence: "Sessizlik",
  },

  home: {
    title: "Gösterge Paneli",
    subtitle: "Nöbet özeti",
    pendingLiveViewApprovals: "Onay Bekleyen Canlı İzleme",
    pendingLiveViewApprovalsDesc: "İK onayı bekleyen talepler",
    pendingDsrs: "Bekleyen Veri Talepleri",
    pendingDsrsDesc: "Yanıt bekleyen m.11 talepleri",
    silenceAlerts: "Sessizlik Uyarıları",
    silenceAlertsDesc: "Son 24 saatte tespit edilen boşluklar",
    recentAudit: "Son Denetim Olayları",
    recentAuditDesc: "Denetim izindeki son olaylar",
    viewAll: "Tümünü Görüntüle",
  },

  liveView: {
    title: "Canlı İzleme Onayları",
    subtitle: "İK onayı bekleyen talepler",
    noRequests: "Onay bekleyen talep yok.",
    approve: "Onayla",
    reject: "Reddet",
    selfApprovalBlocked: "Kendi talebinizi onaylayamazsınız.",
    selfApprovalTooltip:
      "İkili kontrol: Talep sahibi ile onaylayan farklı kullanıcılar olmalıdır.",
    requester: "Talep Eden",
    targetEndpoint: "Hedef Uç Nokta",
    reasonCode: "Neden Kodu",
    requestedAt: "Talep Tarihi",
    duration: "İstenen Süre",
    durationUnit: "dakika",
    approveConfirm: "Bu canlı izleme oturumunu onaylıyor musunuz?",
    rejectConfirm: "Bu talebi reddetmek istediğinizden emin misiniz?",
    rejectReason: "Red gerekçesi (isteğe bağlı)",
    approveSuccess: "Canlı izleme oturumu onaylandı.",
    rejectSuccess: "Canlı izleme talebi reddedildi.",
    approving: "Onaylanıyor...",
    rejecting: "Reddediliyor...",
    states: {
      REQUESTED: "Talep Edildi",
      APPROVED: "Onaylandı",
      ACTIVE: "Aktif",
      ENDED: "Sonlandırıldı",
      DENIED: "Reddedildi",
      EXPIRED: "Süresi Doldu",
      FAILED: "Başarısız",
      TERMINATED_BY_HR: "İK Tarafından Kapatıldı",
      TERMINATED_BY_DPO: "VKG Tarafından Kapatıldı",
    },
    kvkkNotice:
      "Canlı izleme yalnızca meşru amaçlarla ve orantılılık ilkesi çerçevesinde kullanılabilir. Her oturum denetim izine kaydedilir.",
    dualControlNote:
      "Bu talep İK rolündeki farklı bir kullanıcı tarafından onaylanmalıdır (ikili kontrol).",
  },

  dsr: {
    title: "Veri Talepleri (KVKK m.11)",
    subtitle: "Açık ve süresi riskli talepler",
    noPendingDsrs: "Bekleyen veri talebi yok.",
    respond: "Yanıtla",
    responding: "Yanıtlanıyor...",
    respondSuccess: "Yanıt kaydedildi. Talep kapatıldı.",
    respondTitle: "Talebi Yanıtla",
    artifactLabel: "Yanıt Belgesi Referansı",
    artifactPlaceholder: "örn. dsr-responses/2024/uuid.pdf",
    artifactNote:
      "Yanıt artefaktı ham kişisel veri içermemelidir. Yalnızca MinIO nesne yolu veya şifreli belge referansı girilmelidir.",
    notesLabel: "Notlar (isteğe bağlı)",
    slaDeadline: "SLA Son Tarihi",
    daysRemaining: "{days} gün kaldı",
    daysOverdue: "{days} gün geçti (SLA İhlali)",
    employee: "Çalışan",
    requestType: "Talep Türü",
    submittedAt: "Başvuru Tarihi",
    types: {
      access: "Bilgi Talebi",
      rectify: "Düzeltme",
      erase: "Silme",
      object: "İtiraz",
      restrict: "Kısıtlama",
      portability: "Taşınabilirlik",
    },
    states: {
      open: "Açık",
      at_risk: "Riskli (20+ gün)",
      overdue: "Süre Aşımı (30+ gün)",
      resolved: "Çözümlendi",
      rejected: "Reddedildi",
    },
  },

  silence: {
    title: "Sessizlik Yönetimi",
    subtitle: "Ajan sinyal boşlukları (Flow 7)",
    description:
      "Uç noktaların beklenen sinyal aralığını aşan boşluklarını görüntüleyin ve gerekçe kodlarıyla onaylayın.",
    noAlerts: "Aktif sessizlik uyarısı bulunmamaktadır.",
    gapStarted: "Boşluk Başlangıcı",
    gapEnded: "Boşluk Bitişi",
    duration: "Süre",
    acknowledged: "Onaylandı",
    notAcknowledged: "Onaylanmadı",
    reason: "Gerekçe",
    acknowledgeTitle: "Sessizlik Boşluğunu Onayla",
    acknowledgeReason: "Gerekçe kodu",
    acknowledgeReasonHint: "Örn: bakım, tatil, cihaz arızası",
    acknowledgeConfirm: "Onayla",
    acknowledgeSuccess: "Sessizlik boşluğu onaylandı.",
    endpoint: "Uç Nokta",
    last24h: "Son 24 Saat",
  },

  notifications: {
    liveViewRequest: "Yeni canlı izleme onay talebi",
    liveViewRequestBody: "{count} adet onay bekleniyor",
    dsrNew: "Yeni veri talebi",
    dsrNewBody: "{count} adet veri talebi bekliyor",
    silenceAlert: "Sessizlik uyarısı",
    silenceAlertBody: "{count} adet sessizlik boşluğu tespit edildi",
    auditSpike: "Denetim izi ani artışı",
    auditSpikeBody: "Son saatte olağandışı aktivite tespit edildi",
  },

  errors: {
    sessionExpired: "Oturumunuz sona erdi. Lütfen tekrar giriş yapın.",
    approvalFailed:
      "Onay işlemi başarısız. Lütfen sayfayı yenileyip tekrar deneyin.",
    rejectFailed:
      "Red işlemi başarısız. Lütfen sayfayı yenileyip tekrar deneyin.",
    respondFailed:
      "Yanıt gönderilemedi. Lütfen sayfayı yenileyip tekrar deneyin.",
    pushRegistrationFailed:
      "Bildirim kaydı başarısız. Push bildirimleri alınamayabilir.",
    biometricFailed:
      "Biyometrik doğrulama başarısız. İşlem iptal edildi.",
    biometricNotAvailable:
      "Biyometrik doğrulama bu cihazda kullanılamıyor.",
  },
} as const;

export type TrKeys = typeof tr;
