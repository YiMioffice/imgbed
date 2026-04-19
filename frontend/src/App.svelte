<script lang="ts">
  import { onMount } from 'svelte';

  let path = window.location.pathname;
  let parts: string[] = [];
  let resourceDetailId = '';
  let isDashboardPage = false;
  let isPolicyPage = false;
  let isResourcePage = false;
  let isResourceDetailPage = false;
  let isUserGroupPage = false;
  let isUserPage = false;
  let isStoragePage = false;
  let isSiteSettingsPage = false;
  let isCompressionPage = false;
  let isFeaturedAdminPage = false;
  let isExplorePage = false;
  let isAdminPage = false;
  let isAdminProtectedPage = false;
  let isAccountPage = false;

  type AuthUser = { id: string; username: string; displayName: string; role: string; groupId: string; groupName: string; status: string };
  type InstallState = { initialized: boolean; siteName: string; defaultStorage: string; adminUsername: string };
  type PolicyGroup = { id: string; name: string; description: string; defaultDeliveryRouteId?: string; allowedDeliveryRouteIds?: string[]; allowDeliveryRouteSelection: boolean; isActive: boolean; isDefault: boolean; createdAt: string; updatedAt: string };
  type PolicyRule = { userGroup: string; resourceType: string; extension?: string; allowUpload: boolean; allowAccess: boolean; maxFileSizeBytes: number; monthlyTrafficPerResourceBytes: number; monthlyTrafficPerUserAndTypeBytes: number; requireAuth: boolean; requireReview: boolean; forcePrivate: boolean; cacheControl?: string; downloadDisposition?: string };
  type PolicyDecision = { allowed: boolean; reason: string; rule: PolicyRule };
  type PolicyTestResponse = { metadata: { filename: string; extension: string; type: string; contentType: string; size: number }; decision: PolicyDecision; policyGroup: PolicyGroup };
  type ResourceRecord = { id: string; userGroup: string; isPrivate: boolean; storageDriver: string; objectKey: string; deliveryRouteId?: string; publicUrl: string; originalName: string; extension: string; type: string; size: number; contentType: string; hash: string; status: string; accessCount: number; trafficBytes: number; monthlyTraffic: number; monthlyLimit: number; monthWindow: string; cacheControl?: string; disposition?: string; createdAt: string; updatedAt: string; uploadIp?: string; uploadUserAgent?: string };
  type ResourceLinks = { direct: string; markdown: string; html: string; bbcode: string };
  type ResourceDetail = { record: ResourceRecord; metadata: { resourceId: string; headerSha256: string; imageWidth: number; imageHeight: number; imageDecoded: boolean }; variants: Array<{ id: string; kind: string; objectKey: string; contentType: string; size: number; width: number; height: number }>; links: ResourceLinks; trafficWindows: Array<{ windowType: string; windowKey: string; requestCount: number; trafficBytes: number }> };
  type SignedLinkResult = { url: string; expiresAt: string };
  type UploadItemResponse = { success: boolean; status: number; filename: string; metadata: { filename: string; extension: string; type: string; contentType: string; size: number }; resource?: ResourceRecord; links?: ResourceLinks; decision?: PolicyDecision; compression?: { applied: boolean; originalBytes: number; compressedBytes: number; quality: number; ratio: number }; error?: { code: string; message: string } };
  type UploadQueueItem = { name: string; size: number; progress: number; status: 'pending' | 'uploading' | 'success' | 'error'; resource?: ResourceRecord; links?: ResourceLinks; message?: string; errorCode?: string };
  type OverviewStats = { totalResources: number; activeResources: number; totalStorageBytes: number; totalTrafficBytes: number; todayUploads: number; recentTraffic: Array<{ label: string; bytes: number }> };
  type UserGroup = { id: string; name: string; description: string; totalCapacityBytes: number; defaultMonthlyTrafficBytes: number; maxFileSizeBytes: number; dailyUploadLimit: number; dailyIpUploadLimit: number; allowHotlink: boolean; imageCompressionEnabled: boolean; imageCompressionQuality: number; createdAt: string; updatedAt: string };
  type AccountUsage = { user?: AuthUser | null; group: UserGroup; usedStorageBytes: number; monthlyTrafficBytes: number; dailyUploadCount: number };
  type ManagedUser = AuthUser;
  type StorageConfig = { id: string; type: string; name: string; endpoint: string; region: string; bucket: string; accessKeyId: string; secretAccessKey?: string; username?: string; password?: string; publicBaseUrl: string; basePath: string; usePathStyle: boolean; isDefault: boolean; createdAt?: string; updatedAt?: string };
  type DeliveryRoute = { id: string; name: string; description: string; publicBaseUrl: string; isDefault: boolean; isEnabled: boolean; createdAt?: string; updatedAt?: string };
  type SiteSettings = { siteName: string; externalBaseUrl: string; allowGuestUploads: boolean; showStatsOnHome: boolean; showFeaturedOnHome: boolean; updatedAt?: string };
  type FeaturedResource = { resource: ResourceRecord; sortOrder: number; createdAt: string; updatedAt: string };

  const groupOptions = ['guest', 'user', 'admin'];
  const resourceTypeOptions = ['image', 'script', 'stylesheet', 'archive', 'executable', 'document', 'font', 'video', 'other'];
  const resourcePageSizeOptions = [8, 12, 24];
  const homeFeaturedLimit = 6;
  const exploreFeaturedPageSize = 12;
  const megabyte = 1024 * 1024;
  const gigabyte = 1024 * 1024 * 1024;
  const groupLabels: Record<string, string> = { guest: '游客', user: '登录用户', admin: '管理员' };
  const resourceTypeLabels: Record<string, string> = {
    image: '图片',
    script: '脚本',
    stylesheet: '样式表',
    archive: '压缩包',
    executable: '可执行文件',
    document: '文档',
    font: '字体',
    video: '视频',
    other: '其他'
  };
  function resolveRouteState(pathname: string) {
    const segments = pathname.split('/').filter(Boolean);
    const detailId = segments[0] === 'admin' && segments[1] === 'resources' && segments[2] ? decodeURIComponent(segments[2]) : '';
    const dashboard = pathname === '/admin' || pathname === '/admin/dashboard';
    const policy = pathname === '/admin/policies';
    const resource = pathname === '/admin/resources';
    const resourceDetail = detailId !== '';
    const userGroup = pathname === '/admin/user-groups';
    const user = pathname === '/admin/users';
    const storage = pathname === '/admin/storage';
    const siteSettings = pathname === '/admin/site';
    const compression = pathname === '/admin/compression';
    const featuredAdmin = pathname === '/admin/featured';
    const explore = pathname === '/explore';
    const admin = dashboard || policy || resource || userGroup || user || storage || siteSettings || compression || featuredAdmin;
    return {
      parts: segments,
      resourceDetailId: detailId,
      isDashboardPage: dashboard,
      isPolicyPage: policy,
      isResourcePage: resource,
      isResourceDetailPage: resourceDetail,
      isUserGroupPage: userGroup,
      isUserPage: user,
      isStoragePage: storage,
      isSiteSettingsPage: siteSettings,
      isCompressionPage: compression,
      isFeaturedAdminPage: featuredAdmin,
      isExplorePage: explore,
      isAdminPage: admin,
      isAdminProtectedPage: admin || resourceDetail,
      isAccountPage: pathname === '/account'
    };
  }
  const emptyRule = (userGroup: string, resourceType: string): PolicyRule => ({ userGroup, resourceType, extension: '', allowUpload: false, allowAccess: false, maxFileSizeBytes: 0, monthlyTrafficPerResourceBytes: 0, monthlyTrafficPerUserAndTypeBytes: 0, requireAuth: false, requireReview: false, forcePrivate: false, cacheControl: '', downloadDisposition: '' });
  const defaultSiteSettings = (): SiteSettings => ({ siteName: '马赫环', externalBaseUrl: '', allowGuestUploads: true, showStatsOnHome: true, showFeaturedOnHome: true });

  let siteName = '马赫环';
  let siteSettings: SiteSettings = defaultSiteSettings();
  let siteSettingsForm: SiteSettings = defaultSiteSettings();
  let installState: InstallState | null = null;
  let installReady = false;
  let installLoadError = '';
  let currentUser: AuthUser | null = null;
  let authReady = false;
  let bootstrapReady = false;
  let homeStats: OverviewStats = { totalResources: 0, activeResources: 0, totalStorageBytes: 0, totalTrafficBytes: 0, todayUploads: 0, recentTraffic: [] };
  let homeStatsReady = false;
  let featuredResources: FeaturedResource[] = [];
  let featuredReady = false;
  let explorePage = 1;
  let galleryModalResource: ResourceRecord | null = null;

  let installForm = { siteName: '马赫环', defaultStorage: 'local', adminUsername: 'owner', displayName: '管理员', password: '', confirmPassword: '' };
  let installError = '';
  let isInitializing = false;

  let loginForm = { username: '', password: '' };
  let loginError = '';
  let isLoggingIn = false;

  let uploadGroup = 'guest';
  let uploadFiles: File[] = [];
  let uploadQueue: UploadQueueItem[] = [];
  let uploadError = '';
  let uploadProgress = 0;
  let uploadSpeedBps = 0;
  let isUploading = false;
  let isDragging = false;

  let policyGroups: PolicyGroup[] = [];
  let activePolicyGroupId = '';
  let selectedPolicyGroupId = '';
  let policyGroupForm = { name: '', description: '' };
  let policyGroupError = '';
  let isCreatingPolicyGroup = false;
  let rulesJson = '[]';
  let matrixBaseRules: PolicyRule[] = [];
  let matrixOverrideRules: PolicyRule[] = [];
  let matrixError = '';
  let policySaveError = '';
  let policySaveMessage = '';
  let isSavingPolicies = false;
  let policyForm = { action: 'upload', group: 'guest', filename: 'demo.jpg', contentType: 'image/jpeg', size: 1 };
  let policyResult: PolicyTestResponse | null = null;
  let policyError = '';
  let isTestingPolicy = false;

  let resourceFilters = { search: '', type: '', status: 'active', userGroup: '', sort: 'created_desc' };
  let resources: ResourceRecord[] = [];
  let resourcePage = 1;
  let resourcePageSize = resourcePageSizeOptions[0];
  let resourceTotal = 0;
  let resourceTotalPages = 0;
  let resourceError = '';
  let resourceMessage = '';
  let isLoadingResources = false;

  let resourceDetail: ResourceDetail | null = null;
  let detailError = '';
  let isLoadingDetail = false;
  let copyMessage = '';
  let signedLinkResult: SignedLinkResult | null = null;
  let signedLinkExpiresInSeconds = 3600;
  let accountUsage: AccountUsage | null = null;
  let accountError = '';
  let userGroups: UserGroup[] = [];
  let userGroupError = '';
  let userGroupMessage = '';
  let savingUserGroupId = '';
  let managedUsers: ManagedUser[] = [];
  let userAdminError = '';
  let userAdminMessage = '';
  let isCreatingUser = false;
  let savingManagedUserId = '';
  let createUserForm = { username: '', displayName: '', password: '', groupId: 'user', status: 'active' };
  let storageConfigs: StorageConfig[] = [];
  let storageError = '';
  let storageMessage = '';
  let savingStorageId = '';
  let checkingStorageId = '';
  let storageHealthResult = '';
  let deliveryRoutes: DeliveryRoute[] = [];
  let uploadDeliveryRoutes: DeliveryRoute[] = [];
  let selectedDeliveryRouteId = '';
  let allowDeliveryRouteSelection = false;
  let deliveryRouteError = '';
  let deliveryRouteMessage = '';
  let savingDeliveryRouteId = '';
  let siteSettingsError = '';
  let siteSettingsMessage = '';
  let isSavingSiteSettings = false;
  let featuredError = '';
  let featuredMessage = '';
  let routeLoadToken = 0;
  let routeTransitionTimer = 0;
  const inflightLoads = new Map<string, Promise<void>>();
  const cacheTTL = {
    install: 60_000,
    siteSettings: 30_000,
    currentUser: 15_000,
    homeStats: 15_000,
    featuredResources: 30_000,
    accountUsage: 15_000,
    userGroups: 20_000,
    users: 20_000,
    policyGroups: 20_000,
    policies: 20_000,
    storageConfigs: 20_000,
    deliveryRoutes: 20_000,
    deliveryRouteChoices: 20_000,
    resources: 10_000,
    resourceDetail: 10_000
  } as const;
  const cacheTimestamps = {
    install: 0,
    siteSettings: 0,
    currentUser: 0,
    homeStats: 0,
    featuredResources: 0,
    accountUsage: 0,
    userGroups: 0,
    users: 0,
    policyGroups: 0,
    storageConfigs: 0,
    deliveryRoutes: 0,
    deliveryRouteChoices: 0,
    resources: 0
  };
  let resourcesCacheKey = '';
  const policyCacheTimestamps = new Map<string, number>();
  const resourceDetailCacheTimestamps = new Map<string, number>();

  $: ({ parts, resourceDetailId, isDashboardPage, isPolicyPage, isResourcePage, isResourceDetailPage, isUserGroupPage, isUserPage, isStoragePage, isSiteSettingsPage, isCompressionPage, isFeaturedAdminPage, isExplorePage, isAdminPage, isAdminProtectedPage, isAccountPage } = resolveRouteState(path));

  $: uploadGroup = currentUser?.groupId ?? 'guest';

  onMount(() => {
    const handlePopState = () => {
      path = window.location.pathname;
      pulseRouteTransition();
      void handleRouteChange();
    };
    window.addEventListener('popstate', handlePopState);
    void (async () => {
      await Promise.all([loadInstallState(), loadSiteSettings(true)]);
      await loadCurrentUser();
      bootstrapReady = true;
      await handleRouteChange();
    })();
    return () => {
      window.removeEventListener('popstate', handlePopState);
      if (routeTransitionTimer) window.clearTimeout(routeTransitionTimer);
      document.documentElement.classList.remove('route-transitioning');
    };
  });

  async function navigate(url: string, replace = false) {
    if (url === path) return;
    window.history[replace ? 'replaceState' : 'pushState']({}, '', url);
    path = window.location.pathname;
    pulseRouteTransition();
    window.scrollTo({ top: 0, behavior: 'auto' });
    await handleRouteChange();
  }

  function pulseRouteTransition() {
    document.documentElement.classList.remove('route-transitioning');
    void document.documentElement.offsetWidth;
    document.documentElement.classList.add('route-transitioning');
    if (routeTransitionTimer) window.clearTimeout(routeTransitionTimer);
    routeTransitionTimer = window.setTimeout(() => {
      document.documentElement.classList.remove('route-transitioning');
      routeTransitionTimer = 0;
    }, 260);
  }

  function jump(url: string) { void navigate(url); }
  function isFresh(timestamp: number, ttl: number) { return timestamp > 0 && Date.now() - timestamp < ttl; }
  function runDeduped(key: string, task: () => Promise<void>) {
    const pending = inflightLoads.get(key);
    if (pending) return pending;
    const next = task().finally(() => inflightLoads.delete(key));
    inflightLoads.set(key, next);
    return next;
  }
  function invalidateCache(...keys: Array<keyof typeof cacheTimestamps>) {
    for (const key of keys) cacheTimestamps[key] = 0;
  }
  function currentResourcesKey(featured = isFeaturedAdminPage) {
    return JSON.stringify({
      page: resourcePage,
      pageSize: resourcePageSize,
      filters: resourceFilters,
      featured
    });
  }
  function isKnownUninitialized() {
    return installReady && installState?.initialized === false && !currentUser && !installLoadError;
  }

  async function handleRouteChange() {
    const route = resolveRouteState(path);
    const token = ++routeLoadToken;
    const initialized = installState?.initialized || !!currentUser;
    const knownUninitialized = isKnownUninitialized();

    if (knownUninitialized && (path === '/login' || path === '/account' || path.startsWith('/admin'))) {
      await navigate('/install', true);
      return;
    }
    if (initialized && path === '/install') {
      await navigate(currentUser ? (currentUser.role === 'admin' ? '/admin' : '/account') : '/login', true);
      return;
    }
    if ((path === '/login' || path === '/install') && currentUser) {
      await navigate(currentUser.role === 'admin' ? '/admin' : '/account', true);
      return;
    }
    if (route.isAdminProtectedPage && authReady && (!currentUser || currentUser.role !== 'admin')) {
      await navigate(currentUser ? '/account' : '/login', true);
      return;
    }

    const tasks: Promise<unknown>[] = [];
    if (path === '/' || route.isDashboardPage) tasks.push(loadHomeStats());
    if (path === '/' || route.isExplorePage || route.isFeaturedAdminPage) tasks.push(loadFeaturedResources(true));
    if (path === '/upload' || route.isAccountPage) tasks.push(loadAccountUsage());
    if (path === '/upload') tasks.push(loadDeliveryRouteChoices());

    if (currentUser?.role === 'admin' && route.isAdminProtectedPage) {
      if (route.isDashboardPage) tasks.push(loadDashboardData());
      if (route.isPolicyPage) tasks.push(loadPolicyEditor());
      if (route.isPolicyPage) tasks.push(loadDeliveryRoutes());
      if (route.isUserGroupPage) tasks.push(loadUserGroups());
      if (route.isUserPage) tasks.push(loadUserAdminData());
      if (route.isStoragePage) tasks.push(loadStorageConfigs());
      if (route.isSiteSettingsPage) tasks.push(loadSiteSettings());
      if (route.isCompressionPage) tasks.push(loadUserGroups());
      if (route.isFeaturedAdminPage) tasks.push(loadResources(resourcePage, false, true));
      if (route.isResourcePage) tasks.push(loadResources(resourcePage, false, false));
      if (route.isResourceDetailPage) tasks.push(loadResourceDetail(route.resourceDetailId));
    }

    await Promise.all(tasks);
    if (token !== routeLoadToken) return;
  }
  async function loadInstallState(force = false) {
    if (!force && installReady && isFresh(cacheTimestamps.install, cacheTTL.install)) return;
    return runDeduped('install', async () => {
      try {
        const res = await fetch('/api/v1/install/state');
        const payload = await readJSON(res) as InstallState & { error?: string; detail?: string };
        if (!res.ok) {
          installLoadError = payload.error ?? payload.detail ?? '加载初始化状态失败';
          installState = null;
          return;
        }
        installLoadError = '';
        installState = payload;
        siteName = payload.siteName || siteName;
        installForm.siteName = payload.siteName || installForm.siteName;
        installForm.defaultStorage = payload.defaultStorage || installForm.defaultStorage;
        if (payload.adminUsername) loginForm.username = payload.adminUsername;
        cacheTimestamps.install = Date.now();
      } finally {
        installReady = true;
      }
    });
  }
  async function loadSiteSettings(silent = false) {
    if (isFresh(cacheTimestamps.siteSettings, cacheTTL.siteSettings)) return;
    if (!silent) {
      siteSettingsError = '';
      siteSettingsMessage = '';
    }
    return runDeduped('siteSettings', async () => {
      try {
        const res = await fetch('/api/v1/site-settings');
        const payload = await res.json();
        if (!res.ok) return void (!silent && (siteSettingsError = payload.error ?? '加载站点设置失败'));
        siteSettings = { ...defaultSiteSettings(), ...(payload.settings ?? {}) };
        siteSettingsForm = { ...siteSettings };
        siteName = siteSettings.siteName || siteName;
        cacheTimestamps.siteSettings = Date.now();
      } catch (error) {
        if (!silent) siteSettingsError = error instanceof Error ? error.message : '加载站点设置失败';
      }
    });
  }
  async function loadCurrentUser(force = false) {
    if (!force && authReady && isFresh(cacheTimestamps.currentUser, cacheTTL.currentUser)) return;
    return runDeduped('currentUser', async () => {
      try {
        const res = await fetch('/api/v1/auth/me');
        currentUser = res.ok ? ((await res.json()).user ?? null) : null;
        cacheTimestamps.currentUser = Date.now();
      } finally {
        authReady = true;
      }
    });
  }
  async function loadHomeStats(force = false) {
    if (!force && homeStatsReady && isFresh(cacheTimestamps.homeStats, cacheTTL.homeStats)) return;
    return runDeduped('homeStats', async () => {
      try {
        const res = await fetch('/api/v1/stats/overview');
        if (!res.ok) return;
        homeStats = (await res.json()).stats ?? homeStats;
        cacheTimestamps.homeStats = Date.now();
      } finally {
        homeStatsReady = true;
      }
    });
  }
  async function loadFeaturedResources(silent = false) {
    if (featuredReady && isFresh(cacheTimestamps.featuredResources, cacheTTL.featuredResources)) return;
    if (!silent) {
      featuredError = '';
      featuredMessage = '';
    }
    return runDeduped('featuredResources', async () => {
      try {
        const res = await fetch('/api/v1/featured-resources');
        const payload = await res.json();
        if (!res.ok) return void (!silent && (featuredError = payload.error ?? '加载精选资源失败'));
        featuredResources = payload.items ?? [];
        cacheTimestamps.featuredResources = Date.now();
      } catch (error) {
        if (!silent) featuredError = error instanceof Error ? error.message : '加载精选资源失败';
      } finally {
        featuredReady = true;
      }
    });
  }
  async function loadAccountUsage(force = false) {
    if (!force && isFresh(cacheTimestamps.accountUsage, cacheTTL.accountUsage)) return;
    accountError = '';
    return runDeduped('accountUsage', async () => {
      try {
        const res = await fetch('/api/v1/account/usage');
        const payload = await res.json();
        if (!res.ok) return void (accountError = payload.error ?? '加载账户用量失败');
        accountUsage = payload.usage ?? null;
        cacheTimestamps.accountUsage = Date.now();
      } catch (error) {
        accountError = error instanceof Error ? error.message : '加载账户用量失败';
      }
    });
  }
  async function loadDashboardData() { await Promise.all([loadUserGroups(true), loadUsers(true)]); }
  async function saveSiteSettings() {
    siteSettingsError = '';
    siteSettingsMessage = '';
    isSavingSiteSettings = true;
    try {
      const res = await fetch('/api/v1/site-settings', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          siteName: siteSettingsForm.siteName,
          externalBaseUrl: siteSettingsForm.externalBaseUrl,
          allowGuestUploads: !!siteSettingsForm.allowGuestUploads,
          showStatsOnHome: !!siteSettingsForm.showStatsOnHome,
          showFeaturedOnHome: !!siteSettingsForm.showFeaturedOnHome
        })
      });
      const payload = await res.json();
      if (!res.ok) return void (siteSettingsError = payload.error ?? '保存站点设置失败');
      siteSettings = { ...defaultSiteSettings(), ...(payload.settings ?? {}) };
      siteSettingsForm = { ...siteSettings };
      siteName = siteSettings.siteName || siteName;
      siteSettingsMessage = '站点设置已保存。';
      invalidateCache('siteSettings', 'install');
      await loadInstallState(true);
    } catch (error) {
      siteSettingsError = error instanceof Error ? error.message : '保存站点设置失败';
    } finally {
      isSavingSiteSettings = false;
    }
  }

  async function initializeSite() {
    installError = '';
    if (installLoadError) return void (installError = installLoadError);
    if (!isKnownUninitialized()) return void (installError = '当前系统不是可初始化状态。为避免覆盖已有数据，请先确认后端初始化状态。');
    if (installForm.password.length < 8) return void (installError = '密码至少需要 8 位。');
    if (installForm.password !== installForm.confirmPassword) return void (installError = '两次输入的密码不一致。');
    isInitializing = true;
    try {
      const res = await fetch('/api/v1/install/setup', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ siteName: installForm.siteName, defaultStorage: installForm.defaultStorage, adminUsername: installForm.adminUsername, displayName: installForm.displayName, password: installForm.password }) });
      const payload = await readJSON(res);
      if (!res.ok) return void (installError = payload.error ?? '初始化失败');
      currentUser = payload.user;
      invalidateCache('install', 'currentUser', 'homeStats', 'siteSettings');
      await loadInstallState(true);
      await loadHomeStats(true);
      jump('/admin');
    } catch (error) {
      installError = error instanceof Error ? error.message : '初始化失败';
    } finally {
      isInitializing = false;
    }
  }

  async function login() {
    if (isKnownUninitialized()) return jump('/install');
    isLoggingIn = true;
    loginError = '';
    try {
      const res = await fetch('/api/v1/auth/login', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(loginForm) });
      const payload = await res.json();
      if (!res.ok) return void (loginError = payload.error ?? '登录失败');
      currentUser = payload.user;
      cacheTimestamps.currentUser = Date.now();
      invalidateCache('accountUsage');
      jump(payload.user?.role === 'admin' ? '/admin' : '/account');
    } catch (error) {
      loginError = error instanceof Error ? error.message : '登录失败';
    } finally {
      isLoggingIn = false;
    }
  }

  async function logout() {
    await fetch('/api/v1/auth/logout', { method: 'POST' });
    currentUser = null;
    invalidateCache('currentUser', 'accountUsage');
    if (path.startsWith('/admin')) jump('/login');
  }

  function mergeUploadFiles(files: File[]) {
    if (files.length === 0) return;
    uploadFiles = [...uploadFiles, ...files];
    uploadQueue = uploadFiles.map((file, index) => uploadQueue[index] ?? { name: file.name, size: file.size, progress: 0, status: 'pending' });
    uploadError = '';
  }
  function handleUploadSelection(event: Event) { const target = event.currentTarget as HTMLInputElement; mergeUploadFiles(Array.from(target.files ?? [])); target.value = ''; }
  function removeQueuedFile(index: number) { uploadFiles = uploadFiles.filter((_, current) => current !== index); uploadQueue = uploadQueue.filter((_, current) => current !== index); }
  function handleDrop(event: DragEvent) { event.preventDefault(); isDragging = false; mergeUploadFiles(Array.from(event.dataTransfer?.files ?? [])); }
  function handlePaste(event: ClipboardEvent) { const files = Array.from(event.clipboardData?.files ?? []); if (files.length > 0) { event.preventDefault(); mergeUploadFiles(files); } }

  async function uploadSelectedResources() {
    uploadError = '';
    copyMessage = '';
    if (uploadFiles.length === 0) return void (uploadError = '请选择至少一个资源文件。');
    isUploading = true;
    uploadProgress = 0;
    uploadSpeedBps = 0;
    uploadQueue = uploadFiles.map((file) => ({ name: file.name, size: file.size, progress: 0, status: 'uploading' }));
    try {
      const payload = await uploadWithProgress(uploadFiles, (ratio, speedBps) => {
        uploadProgress = ratio;
        uploadSpeedBps = speedBps;
        uploadQueue = uploadQueue.map((item, index) => ({ ...item, progress: estimateItemProgress(uploadFiles, index, ratio) }));
      });
      const items = (payload.items ?? []) as UploadItemResponse[];
      uploadQueue = uploadQueue.map((item, index) => {
        const result = items[index];
        if (!result) return { ...item, status: 'error', progress: 0, message: '上传结果缺失' };
        return { ...item, progress: result.success ? 1 : 0, status: result.success ? 'success' : 'error', resource: result.resource, links: result.links, message: result.success ? uploadSuccessMessage(result) : translateSystemMessage(result.error?.message || result.decision?.reason || '上传失败'), errorCode: result.error?.code };
      });
      uploadProgress = 1;
      invalidateCache('homeStats', 'accountUsage', 'resources', 'featuredResources');
      await loadHomeStats(true);
      await loadAccountUsage(true);
      if (isResourcePage) await loadResources(resourcePage, true);
    } catch (error) {
      uploadError = error instanceof Error ? error.message : '上传失败';
      uploadQueue = uploadQueue.map((item) => ({ ...item, status: 'error', progress: 0, message: uploadError }));
    } finally {
      isUploading = false;
    }
  }

  function uploadWithProgress(files: File[], onProgress: (ratio: number, speedBps: number) => void) {
    return new Promise<any>((resolve, reject) => {
      const xhr = new XMLHttpRequest();
      const startedAt = performance.now();
      xhr.open('POST', '/api/v1/resources/upload');
      xhr.responseType = 'text';
      xhr.upload.onprogress = (event) => {
        if (event.lengthComputable && event.total > 0) {
          const elapsedSeconds = Math.max((performance.now() - startedAt) / 1000, 0.1);
          onProgress(event.loaded / event.total, event.loaded / elapsedSeconds);
        }
      };
      xhr.onload = () => {
        const payload = parseUploadResponse(xhr);
        if (xhr.status >= 200 && xhr.status < 300) return resolve(payload);
        reject(new Error(uploadHTTPErrorMessage(xhr.status, payload)));
      };
      xhr.onerror = () => reject(new Error('上传请求失败'));
      const form = new FormData();
      if (selectedDeliveryRouteId) form.append('deliveryRouteId', selectedDeliveryRouteId);
      files.forEach((file) => form.append('files', file, file.name));
      xhr.send(form);
    });
  }

  function parseUploadResponse(xhr: XMLHttpRequest) {
    const text = xhr.responseText?.trim() ?? '';
    if (!text) return {};
    try {
      return JSON.parse(text);
    } catch {
      return { error: { message: uploadHTTPErrorMessage(xhr.status, null) } };
    }
  }

  function uploadHTTPErrorMessage(status: number, payload: any) {
    const message = payload?.error?.message || payload?.error;
    if (message) return translateSystemMessage(message);
    if (status === 413) return '上传体积超过网关或反向代理限制，请调高 client_max_body_size / 平台上传限制后重试。';
    if (status === 429) return '上传请求过于频繁，请稍后重试。';
    if (status >= 500) return '服务器处理上传失败，请检查后端或存储服务状态。';
    if (status > 0) return `上传失败：HTTP ${status}`;
    return '上传请求失败';
  }

  function estimateItemProgress(files: File[], index: number, ratio: number) {
    const total = files.reduce((sum, file) => sum + Math.max(file.size, 1), 0);
    const loaded = total * ratio;
    let offset = 0;
    for (let current = 0; current < files.length; current += 1) {
      const size = Math.max(files[current].size, 1);
      if (current === index) return Math.max(0, Math.min(1, (loaded - offset) / size));
      offset += size;
    }
    return ratio;
  }

  function uploadSuccessMessage(result: UploadItemResponse) {
    if (result.compression?.applied) {
      return `上传完成，已压缩 ${formatBytes(result.compression.originalBytes)} → ${formatBytes(result.compression.compressedBytes)}。`;
    }
    return translateSystemMessage(result.decision?.reason || '上传完成');
  }

  function formatUploadSpeed(value: number) { return value > 0 ? `${formatBytes(value)}/s` : '计算中'; }

  async function loadPolicyGroups(force = false) { if (!force && isFresh(cacheTimestamps.policyGroups, cacheTTL.policyGroups)) return; policyGroupError = ''; return runDeduped('policyGroups', async () => { try { const res = await fetch('/api/v1/policy-groups'); const payload = await res.json(); if (!res.ok) return void (policyGroupError = payload.error ?? '加载策略组失败'); policyGroups = payload.groups ?? []; activePolicyGroupId = payload.activeGroup?.id ?? ''; if (!selectedPolicyGroupId || !policyGroups.some((group) => group.id === selectedPolicyGroupId)) selectedPolicyGroupId = activePolicyGroupId || policyGroups[0]?.id || ''; cacheTimestamps.policyGroups = Date.now(); } catch (error) { policyGroupError = error instanceof Error ? error.message : '加载策略组失败'; } }); }
  async function loadPolicies(groupId = selectedPolicyGroupId, force = false) { const resolved = groupId || activePolicyGroupId; if (!resolved) return; if (!force && policyCacheTimestamps.has(resolved) && isFresh(policyCacheTimestamps.get(resolved) ?? 0, cacheTTL.policies)) return; policySaveError = ''; policySaveMessage = ''; return runDeduped(`policies:${resolved}`, async () => { try { const res = await fetch(`/api/v1/policies?groupId=${encodeURIComponent(resolved)}`); const payload = await res.json(); if (!res.ok) return void (policySaveError = payload.error ?? '加载策略失败'); selectedPolicyGroupId = payload.group?.id ?? resolved; rulesJson = JSON.stringify(payload.rules ?? [], null, 2); syncMatrixFromRules(payload.rules ?? []); policyCacheTimestamps.set(resolved, Date.now()); } catch (error) { policySaveError = error instanceof Error ? error.message : '加载策略失败'; } }); }
  async function loadPolicyEditor(groupId = selectedPolicyGroupId) {
    await loadPolicyGroups();
    const resolved = groupId || selectedPolicyGroupId || activePolicyGroupId || policyGroups[0]?.id || '';
    if (!resolved) return;
    await loadPolicies(resolved);
  }
  function syncMatrixFromRules(rules: PolicyRule[]) { const baseMap = new Map<string, PolicyRule>(); const overrides: PolicyRule[] = []; for (const rule of rules) { if (rule.extension) overrides.push({ ...rule }); else baseMap.set(`${rule.userGroup}|${rule.resourceType}`, { ...rule, extension: '' }); } matrixBaseRules = []; for (const group of groupOptions) for (const type of resourceTypeOptions) matrixBaseRules.push(baseMap.get(`${group}|${type}`) ?? emptyRule(group, type)); matrixOverrideRules = overrides; matrixError = ''; }
  function collectMatrixRules() { return [...matrixBaseRules, ...matrixOverrideRules].map((rule) => ({ ...rule, extension: (rule.extension ?? '').trim().replace(/^\./, '').toLowerCase(), cacheControl: (rule.cacheControl ?? '').trim(), downloadDisposition: (rule.downloadDisposition ?? '').trim() })).filter((rule) => rule.userGroup && rule.resourceType); }
  function validateMatrixRules(rules: PolicyRule[]) { const errors: string[] = []; const seen = new Set<string>(); for (const [index, rule] of rules.entries()) { const label = rule.extension ? `扩展规则 ${index + 1}` : `${groupLabel(rule.userGroup)}/${resourceTypeLabel(rule.resourceType)}`; if (!groupOptions.includes(rule.userGroup)) errors.push(`${label} 的用户组无效`); if (!resourceTypeOptions.includes(rule.resourceType)) errors.push(`${label} 的资源类型无效`); if (rule.extension && !/^[a-z0-9]+$/.test(rule.extension)) errors.push(`${label} 的扩展名只能包含小写字母和数字`); if (rule.maxFileSizeBytes < 0 || rule.monthlyTrafficPerResourceBytes < 0 || rule.monthlyTrafficPerUserAndTypeBytes < 0) errors.push(`${label} 的数值必须大于等于 0`); if (rule.downloadDisposition && rule.downloadDisposition !== 'inline' && rule.downloadDisposition !== 'attachment') errors.push(`${label} 的下载策略无效`); const key = `${rule.userGroup}|${rule.resourceType}|${rule.extension ?? ''}`; if (seen.has(key)) errors.push(`${label} 与其他规则重复`); seen.add(key); } return errors; }
  function addOverrideRule() { matrixOverrideRules = [...matrixOverrideRules, { ...emptyRule('guest', 'image'), extension: 'jpg', allowAccess: true }]; }
  function removeOverrideRule(index: number) { matrixOverrideRules = matrixOverrideRules.filter((_, current) => current !== index); }
  function applyAdvancedJson() { matrixError = ''; try { const parsed = JSON.parse(rulesJson); if (!Array.isArray(parsed)) return void (matrixError = '高级 JSON 必须是规则数组。'); const errors = validateMatrixRules(parsed); if (errors.length > 0) return void (matrixError = errors[0]); syncMatrixFromRules(parsed); rulesJson = JSON.stringify(parsed, null, 2); } catch (error) { matrixError = error instanceof Error ? error.message : '高级 JSON 解析失败'; } }
  async function createPolicyGroup() { policyGroupError = ''; if (!policyGroupForm.name.trim()) return void (policyGroupError = '请输入策略组名称。'); isCreatingPolicyGroup = true; try { const res = await fetch('/api/v1/policy-groups', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(policyGroupForm) }); const payload = await res.json(); if (!res.ok) return void (policyGroupError = payload.error ?? '创建策略组失败'); policyGroupForm = { name: '', description: '' }; selectedPolicyGroupId = payload.group.id; await loadPolicyEditor(payload.group.id); } catch (error) { policyGroupError = error instanceof Error ? error.message : '创建策略组失败'; } finally { isCreatingPolicyGroup = false; } }
  async function copyPolicyGroup(group: PolicyGroup) { policyGroupError = ''; try { const res = await fetch(`/api/v1/policy-groups/${group.id}/copy`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name: `${group.name} 副本` }) }); const payload = await res.json(); if (!res.ok) return void (policyGroupError = payload.error ?? '复制策略组失败'); selectedPolicyGroupId = payload.group.id; await loadPolicyEditor(payload.group.id); } catch (error) { policyGroupError = error instanceof Error ? error.message : '复制策略组失败'; } }
  async function setPolicyGroupActive(group: PolicyGroup, active: boolean) { policyGroupError = ''; try { const res = await fetch(`/api/v1/policy-groups/${group.id}/${active ? 'activate' : 'deactivate'}`, { method: 'POST' }); const payload = await res.json(); if (!res.ok) return void (policyGroupError = payload.error ?? '更新策略组状态失败'); await loadPolicyEditor(selectedPolicyGroupId || payload.group.id); } catch (error) { policyGroupError = error instanceof Error ? error.message : '更新策略组状态失败'; } }
  function policyGroupAllowedRouteIds(group: PolicyGroup) { return group.allowedDeliveryRouteIds?.length ? group.allowedDeliveryRouteIds : deliveryRoutes.map((route) => route.id); }
  function togglePolicyGroupRoute(group: PolicyGroup, routeId: string) { const set = new Set(policyGroupAllowedRouteIds(group)); if (set.has(routeId)) set.delete(routeId); else set.add(routeId); group.allowedDeliveryRouteIds = Array.from(set); policyGroups = [...policyGroups]; }
  async function savePolicyGroupSettings(group: PolicyGroup) { policyGroupError = ''; try { const res = await fetch(`/api/v1/policy-groups/${encodeURIComponent(group.id)}`, { method: 'PATCH', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name: group.name, description: group.description, defaultDeliveryRouteId: group.defaultDeliveryRouteId || deliveryRoutes.find((route) => route.isDefault)?.id || 'default', allowedDeliveryRouteIds: policyGroupAllowedRouteIds(group), allowDeliveryRouteSelection: !!group.allowDeliveryRouteSelection }) }); const payload = await res.json(); if (!res.ok) return void (policyGroupError = payload.error ?? '保存策略组设置失败'); policyGroupError = ''; policySaveMessage = '策略组线路设置已保存。'; invalidateCache('policyGroups', 'deliveryRouteChoices'); await loadPolicyGroups(true); await loadDeliveryRouteChoices(true); } catch (error) { policyGroupError = error instanceof Error ? error.message : '保存策略组设置失败'; } }
  async function savePolicies() { isSavingPolicies = true; policySaveError = ''; policySaveMessage = ''; matrixError = ''; try { const parsed = collectMatrixRules(); const errors = validateMatrixRules(parsed); if (errors.length > 0) return void (matrixError = errors[0]); const res = await fetch(`/api/v1/policies?groupId=${encodeURIComponent(selectedPolicyGroupId)}`, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ rules: parsed }) }); const payload = await res.json(); if (!res.ok) return void (policySaveError = payload.validationErrors?.[0]?.message ?? payload.error ?? '保存策略失败'); rulesJson = JSON.stringify(payload.rules ?? [], null, 2); syncMatrixFromRules(payload.rules ?? []); policyCacheTimestamps.delete(selectedPolicyGroupId); cacheTimestamps.policyGroups = 0; policySaveMessage = '策略已保存。'; await loadPolicyGroups(true); } catch (error) { policySaveError = error instanceof Error ? error.message : '保存策略失败'; } finally { isSavingPolicies = false; } }
  async function runPolicyTest() { isTestingPolicy = true; policyError = ''; policyResult = null; try { const res = await fetch('/api/v1/policies/test', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ ...policyForm, size: unitToBytes(String(policyForm.size), megabyte) }) }); const payload = await res.json(); if (!res.ok) return void (policyError = res.status === 401 ? '请先登录管理员账号' : (payload.error ?? '策略测试失败')); policyResult = payload; } catch (error) { policyError = error instanceof Error ? error.message : '策略测试失败'; } finally { isTestingPolicy = false; } }
  async function loadUserGroups(silent = false, force = false) { if (!force && isFresh(cacheTimestamps.userGroups, cacheTTL.userGroups)) return; if (!silent) { userGroupError = ''; userGroupMessage = ''; } return runDeduped('userGroups', async () => { try { const res = await fetch('/api/v1/user-groups'); const payload = await res.json(); if (!res.ok) return void (userGroupError = payload.error ?? '加载用户组失败'); userGroups = payload.groups ?? []; cacheTimestamps.userGroups = Date.now(); } catch (error) { if (!silent) userGroupError = error instanceof Error ? error.message : '加载用户组失败'; } }); }
  async function saveUserGroup(group: UserGroup) { userGroupError = ''; userGroupMessage = ''; savingUserGroupId = group.id; try { const res = await fetch(`/api/v1/user-groups/${encodeURIComponent(group.id)}`, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name: group.name, description: group.description, totalCapacityBytes: Number(group.totalCapacityBytes) || 0, defaultMonthlyTrafficBytes: Number(group.defaultMonthlyTrafficBytes) || 0, maxFileSizeBytes: Number(group.maxFileSizeBytes) || 0, dailyUploadLimit: Number(group.dailyUploadLimit) || 0, dailyIpUploadLimit: Number(group.dailyIpUploadLimit) || 0, allowHotlink: group.allowHotlink, imageCompressionEnabled: !!group.imageCompressionEnabled, imageCompressionQuality: clampCompressionQuality(group.imageCompressionQuality) }) }); const payload = await res.json(); if (!res.ok) return void (userGroupError = payload.error ?? '保存用户组失败'); userGroupMessage = `${payload.group?.name ?? group.name} 已保存。`; invalidateCache('userGroups', 'accountUsage'); await loadUserGroups(true, true); if (accountUsage?.group?.id === group.id) await loadAccountUsage(true); } catch (error) { userGroupError = error instanceof Error ? error.message : '保存用户组失败'; } finally { savingUserGroupId = ''; } }
  async function loadUsers(silent = false, force = false) { if (!force && isFresh(cacheTimestamps.users, cacheTTL.users)) return; if (!silent) { userAdminError = ''; userAdminMessage = ''; } return runDeduped('users', async () => { try { const res = await fetch('/api/v1/users'); const payload = await res.json(); if (!res.ok) return void (userAdminError = payload.error ?? '加载用户失败'); managedUsers = payload.users ?? []; cacheTimestamps.users = Date.now(); } catch (error) { if (!silent) userAdminError = error instanceof Error ? error.message : '加载用户失败'; } }); }
  async function loadUserAdminData() { await loadUserGroups(true); await loadUsers(); }
  async function loadStorageConfigs(force = false) {
    if (!force && isFresh(cacheTimestamps.storageConfigs, cacheTTL.storageConfigs)) return;
    storageError = '';
    storageMessage = '';
    storageHealthResult = '';
    return runDeduped('storageConfigs', async () => {
      try {
        const res = await fetch('/api/v1/storage-configs');
        const payload = await res.json();
        if (!res.ok) return void (storageError = payload.error ?? '加载存储配置失败');
        const configs = payload.configs ?? [];
        const defaultId = payload.defaultConfig?.id ?? 'local';
        storageConfigs = [
          ensureStorageConfig(findStorageConfig(configs, 'local', 'local') ?? { id: 'local', type: 'local', name: '本机存储', usePathStyle: true }, 'local'),
          ensureStorageConfig(findStorageConfig(configs, 's3-default', 's3') ?? { id: 's3-default', type: 's3', name: 'S3 兼容存储', usePathStyle: true }, 's3'),
          ensureStorageConfig(findStorageConfig(configs, 'webdav-default', 'webdav') ?? { id: 'webdav-default', type: 'webdav', name: 'WebDAV 存储', usePathStyle: true }, 'webdav')
        ].map((config) => ({ ...config, isDefault: config.id === defaultId }));
        cacheTimestamps.storageConfigs = Date.now();
      } catch (error) {
        storageError = error instanceof Error ? error.message : '加载存储配置失败';
      }
    });
  }
  async function saveStorageConfig(config: StorageConfig) {
    storageError = '';
    storageMessage = '';
    savingStorageId = config.id;
    try {
      const res = await fetch(`/api/v1/storage-configs/${encodeURIComponent(config.id)}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          type: config.type,
          name: config.name,
          endpoint: config.endpoint,
          region: config.region,
          bucket: config.bucket,
          accessKeyId: config.accessKeyId,
          secretAccessKey: config.secretAccessKey,
          username: config.username,
          password: config.password,
          publicBaseUrl: config.publicBaseUrl,
          basePath: config.basePath,
          usePathStyle: config.usePathStyle,
          isDefault: config.isDefault
        })
      });
      const payload = await res.json();
      if (!res.ok) return void (storageError = payload.error ?? '保存存储配置失败');
      const savedConfig = ensureStorageConfig(payload.config ?? config, config.type);
      storageMessage = `${savedConfig.name || config.name} 已保存${savedConfig.isDefault ? '，已设为默认上传存储' : ''}。`;
      invalidateCache('storageConfigs');
      await loadStorageConfigs(true);
      if (savedConfig.type !== 'local') {
        await runStorageHealthCheck({ ...config, ...savedConfig, secretAccessKey: config.secretAccessKey, password: config.password });
      }
    } catch (error) {
      storageError = error instanceof Error ? error.message : '保存存储配置失败';
    } finally {
      savingStorageId = '';
    }
  }
  async function loadDeliveryRoutes(force = false) {
    if (!force && isFresh(cacheTimestamps.deliveryRoutes, cacheTTL.deliveryRoutes)) return;
    deliveryRouteError = '';
    deliveryRouteMessage = '';
    return runDeduped('deliveryRoutes', async () => {
      try {
        const res = await fetch('/api/v1/delivery-routes');
        const payload = await res.json();
        if (!res.ok) return void (deliveryRouteError = payload.error ?? '加载访问线路失败');
        deliveryRoutes = payload.routes ?? [];
        cacheTimestamps.deliveryRoutes = Date.now();
      } catch (error) {
        deliveryRouteError = error instanceof Error ? error.message : '加载访问线路失败';
      }
    });
  }
  async function loadDeliveryRouteChoices(force = false) {
    if (!force && isFresh(cacheTimestamps.deliveryRouteChoices, cacheTTL.deliveryRouteChoices)) return;
    return runDeduped('deliveryRouteChoices', async () => {
      try {
        const res = await fetch('/api/v1/delivery-routes/choices');
        const payload = await res.json();
        if (!res.ok) return;
        uploadDeliveryRoutes = payload.routes ?? [];
        allowDeliveryRouteSelection = !!payload.allowDeliveryRouteSelection;
        selectedDeliveryRouteId = payload.defaultDeliveryRouteId || uploadDeliveryRoutes.find((route) => route.isDefault)?.id || uploadDeliveryRoutes[0]?.id || '';
        cacheTimestamps.deliveryRouteChoices = Date.now();
      } catch {
        uploadDeliveryRoutes = [];
      }
    });
  }
  async function saveDeliveryRoute(route: DeliveryRoute) {
    deliveryRouteError = '';
    deliveryRouteMessage = '';
    savingDeliveryRouteId = route.id;
    try {
      const res = await fetch(`/api/v1/delivery-routes/${encodeURIComponent(route.id)}`, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(route) });
      const payload = await res.json();
      if (!res.ok) return void (deliveryRouteError = payload.error ?? '保存访问线路失败');
      deliveryRouteMessage = `${payload.route?.name ?? route.name} 已保存。`;
      invalidateCache('deliveryRoutes', 'deliveryRouteChoices');
      await loadDeliveryRoutes(true);
    } catch (error) {
      deliveryRouteError = error instanceof Error ? error.message : '保存访问线路失败';
    } finally {
      savingDeliveryRouteId = '';
    }
  }
  async function addDeliveryRoute() {
    const id = `route-${deliveryRoutes.length + 1}`;
    deliveryRoutes = [...deliveryRoutes, { id, name: '新访问线路', description: '', publicBaseUrl: '', isDefault: false, isEnabled: true }];
  }
  async function runStorageHealthCheck(config: StorageConfig) {
    storageError = '';
    storageHealthResult = `${config.name} 正在健康检查。`;
    checkingStorageId = config.id;
    try {
      const res = await fetch('/api/v1/storage-configs/health-check', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          id: config.id,
          type: config.type,
          name: config.name,
          endpoint: config.endpoint,
          region: config.region,
          bucket: config.bucket,
          accessKeyId: config.accessKeyId,
          secretAccessKey: config.secretAccessKey,
          username: config.username,
          password: config.password,
          publicBaseUrl: config.publicBaseUrl,
          basePath: config.basePath,
          usePathStyle: config.usePathStyle,
          isDefault: config.isDefault
        })
      });
      const payload = await res.json();
      if (!res.ok) { storageHealthResult = ''; return void (storageError = payload.error ?? payload.detail ?? '存储健康检查失败'); }
      storageHealthResult = `${config.name} 健康检查通过。`;
    } catch (error) {
      storageHealthResult = '';
      storageError = error instanceof Error ? error.message : '存储健康检查失败';
    } finally {
      checkingStorageId = '';
    }
  }
  async function createManagedUser() { userAdminError = ''; userAdminMessage = ''; if (!createUserForm.username.trim() || !createUserForm.displayName.trim() || createUserForm.password.length < 8) return void (userAdminError = '请填写账号、昵称和至少 8 位密码。'); isCreatingUser = true; try { const res = await fetch('/api/v1/users', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(createUserForm) }); const payload = await res.json(); if (!res.ok) return void (userAdminError = payload.error ?? '创建用户失败'); createUserForm = { username: '', displayName: '', password: '', groupId: 'user', status: 'active' }; userAdminMessage = '用户已创建。'; invalidateCache('users'); await loadUsers(true, true); } catch (error) { userAdminError = error instanceof Error ? error.message : '创建用户失败'; } finally { isCreatingUser = false; } }
  async function saveManagedUser(user: ManagedUser) { userAdminError = ''; userAdminMessage = ''; savingManagedUserId = user.id; try { const res = await fetch(`/api/v1/users/${encodeURIComponent(user.id)}`, { method: 'PATCH', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ displayName: user.displayName, groupId: user.groupId, status: user.status }) }); const payload = await res.json(); if (!res.ok) return void (userAdminError = payload.error ?? '保存用户失败'); userAdminMessage = `${user.displayName} 已保存。`; invalidateCache('users'); await loadUsers(true, true); } catch (error) { userAdminError = error instanceof Error ? error.message : '保存用户失败'; } finally { savingManagedUserId = ''; } }
  async function toggleUserBan(user: ManagedUser) { const nextStatus = user.status === 'banned' ? 'active' : 'banned'; if (!window.confirm(`${nextStatus === 'banned' ? '确认封禁' : '确认解封'} ${user.displayName} 吗？`)) return; await saveManagedUser({ ...user, status: nextStatus }); }
  async function resetManagedUserPassword(user: ManagedUser) { const password = window.prompt(`为 ${user.displayName} 输入新密码`, ''); if (!password) return; if (password.length < 8) return void (userAdminError = '新密码至少需要 8 位。'); if (!window.confirm(`确认重置 ${user.displayName} 的密码吗？`)) return; userAdminError = ''; userAdminMessage = ''; try { const res = await fetch(`/api/v1/users/${encodeURIComponent(user.id)}/reset-password`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ password }) }); const payload = await res.json(); if (!res.ok) return void (userAdminError = payload.error ?? '重置密码失败'); userAdminMessage = `${user.displayName} 的密码已重置。`; } catch (error) { userAdminError = error instanceof Error ? error.message : '重置密码失败'; } }

  async function loadResources(page = resourcePage, force = false, featured = isFeaturedAdminPage) {
    resourcePage = page;
    const nextKey = currentResourcesKey(featured);
    if (!force && resources.length > 0 && resourcesCacheKey === nextKey && isFresh(cacheTimestamps.resources, cacheTTL.resources)) return;
    isLoadingResources = true;
    resourceError = '';
    return runDeduped(`resources:${nextKey}`, async () => {
      try {
        const params = new URLSearchParams({ page: String(page), pageSize: String(resourcePageSize), sort: resourceFilters.sort });
        if (resourceFilters.search.trim()) params.set('search', resourceFilters.search.trim());
        if (resourceFilters.type) params.set('type', resourceFilters.type);
        if (resourceFilters.status) params.set('status', resourceFilters.status);
        if (resourceFilters.userGroup) params.set('userGroup', resourceFilters.userGroup);
        const res = await fetch(`/api/v1/resources?${params.toString()}`);
        const payload = await res.json();
        if (!res.ok) return void (resourceError = payload.error ?? '加载资源失败');
        resources = payload.items ?? [];
        resourcePage = payload.page ?? 1;
        resourceTotal = payload.total ?? 0;
        resourceTotalPages = payload.totalPages ?? 0;
        resourcesCacheKey = nextKey;
        cacheTimestamps.resources = Date.now();
      } catch (error) {
        resourceError = error instanceof Error ? error.message : '加载资源失败';
      } finally {
        isLoadingResources = false;
      }
    });
  }

  async function loadResourceDetail(id: string, force = false) { if (!id) return; if (!force && resourceDetail?.record.id === id && isFresh(resourceDetailCacheTimestamps.get(id) ?? 0, cacheTTL.resourceDetail)) return; isLoadingDetail = true; detailError = ''; signedLinkResult = null; return runDeduped(`resourceDetail:${id}`, async () => { try { const res = await fetch(`/api/v1/resources/${encodeURIComponent(id)}`); const payload = await res.json(); if (!res.ok) return void (detailError = payload.error ?? '加载资源详情失败'); if (!payload.detail?.record) return void (detailError = '资源详情响应缺少记录。'); resourceDetail = normalizeResourceDetail(payload.detail); resourceDetailCacheTimestamps.set(id, Date.now()); } catch (error) { detailError = error instanceof Error ? error.message : '加载资源详情失败'; } finally { isLoadingDetail = false; } }); }
  function normalizeResourceDetail(detail: ResourceDetail): ResourceDetail {
    return {
      ...detail,
      metadata: detail.metadata ?? { resourceId: detail.record.id, headerSha256: '', imageWidth: 0, imageHeight: 0, imageDecoded: false },
      variants: Array.isArray(detail.variants) ? detail.variants : [],
      links: detail.links ?? { direct: detail.record.publicUrl, markdown: '', html: '', bbcode: '' },
      trafficWindows: Array.isArray(detail.trafficWindows) ? detail.trafficWindows : []
    };
  }
  async function mutateResource(url: string, method: string, successMessage: string) {
    resourceError = '';
    resourceMessage = '';
    const res = await fetch(url, { method });
    const payload = await res.json();
    if (!res.ok) return void (resourceError = payload.error ?? '资源操作失败');
    invalidateCache('resources', 'homeStats', 'featuredResources');
    if (payload.resource?.id) resourceDetailCacheTimestamps.delete(payload.resource.id);
    await loadResources(resourcePage, true);
    if (resourceDetail?.record.id === payload.resource?.id) await loadResourceDetail(resourceDetail.record.id, true);
    await Promise.all([loadHomeStats(true), loadFeaturedResources(true)]);
    resourceMessage = successMessage;
  }
  async function deleteResource(id: string) {
    const item = resources.find((resourceItem) => resourceItem.id === id);
    const label = item?.originalName ?? id;
    if (!window.confirm(`确认删除「${label}」？删除后会从精选中移除，可在已删除列表恢复。`)) return;
    await mutateResource(`/api/v1/resources/${encodeURIComponent(id)}`, 'DELETE', '资源已删除，并已从精选列表移除。');
  }
  const restoreResource = (id: string) => mutateResource(`/api/v1/resources/${encodeURIComponent(id)}/restore`, 'POST', '资源已恢复。');
  async function updateResourceVisibility(id: string, isPrivate: boolean) {
    detailError = '';
    signedLinkResult = null;
    const res = await fetch(`/api/v1/resources/${encodeURIComponent(id)}/visibility`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ isPrivate })
    });
    const payload = await res.json();
    if (!res.ok) return void (detailError = payload.error ?? '更新资源可见性失败');
    invalidateCache('resources', 'featuredResources');
    if (payload.resource?.id) resourceDetailCacheTimestamps.delete(payload.resource.id);
    if (resourceDetail?.record.id === payload.resource?.id) await loadResourceDetail(resourceDetail.record.id, true);
    if (isResourcePage || isFeaturedAdminPage) await loadResources(resourcePage, true);
    await loadFeaturedResources(true);
  }
  async function generateSignedLink(id: string) {
    detailError = '';
    signedLinkResult = null;
    const res = await fetch(`/api/v1/resources/${encodeURIComponent(id)}/signed-link`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ expiresInSeconds: Number(signedLinkExpiresInSeconds) || 3600 })
    });
    const payload = await res.json();
    if (!res.ok) return void (detailError = payload.error ?? '生成签名链接失败');
    signedLinkResult = payload;
  }
  async function applyResourceFilters() { resourcePage = 1; await loadResources(1); }
  async function changeResourcePage(page: number) { if (page < 1 || page > resourceTotalPages) return; resourcePage = page; await loadResources(page); }
  async function changeResourcePageSize(value: string) {
    const nextSize = Number(value);
    if (!resourcePageSizeOptions.includes(nextSize) || nextSize === resourcePageSize) return;
    resourcePageSize = nextSize;
    resourcePage = 1;
    resourcesCacheKey = '';
    invalidateCache('resources');
    await loadResources(1, true, isFeaturedAdminPage);
  }
  async function addFeatured(record: ResourceRecord) {
    featuredError = '';
    featuredMessage = '';
    try {
      const res = await fetch('/api/v1/featured-resources', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ resourceId: record.id })
      });
      const payload = await res.json();
      if (!res.ok) return void (featuredError = payload.error ?? '添加精选失败');
      featuredMessage = `${record.originalName} 已加入精选。`;
      invalidateCache('featuredResources');
      await loadFeaturedResources(true);
    } catch (error) {
      featuredError = error instanceof Error ? error.message : '添加精选失败';
    }
  }
  async function removeFeatured(resourceId: string) {
    featuredError = '';
    featuredMessage = '';
    try {
      const res = await fetch(`/api/v1/featured-resources/${encodeURIComponent(resourceId)}`, { method: 'DELETE' });
      const payload = await res.json();
      if (!res.ok) return void (featuredError = payload.error ?? '下架精选失败');
      featuredMessage = '精选资源已下架。';
      invalidateCache('featuredResources');
      await loadFeaturedResources(true);
    } catch (error) {
      featuredError = error instanceof Error ? error.message : '下架精选失败';
    }
  }
  async function moveFeatured(index: number, direction: -1 | 1) {
    const target = index + direction;
    if (target < 0 || target >= featuredResources.length) return;
    featuredError = '';
    featuredMessage = '';
    const ordered = [...featuredResources];
    const [item] = ordered.splice(index, 1);
    ordered.splice(target, 0, item);
    try {
      const res = await fetch('/api/v1/featured-resources/order', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ resourceIds: ordered.map((entry) => entry.resource.id) })
      });
      const payload = await res.json();
      if (!res.ok) return void (featuredError = payload.error ?? '排序精选失败');
      featuredResources = payload.items ?? ordered;
      featuredMessage = '精选排序已更新。';
    } catch (error) {
      featuredError = error instanceof Error ? error.message : '排序精选失败';
    }
  }

  async function copyText(value: string) {
    const done = (message: string) => {
      copyMessage = message;
      setTimeout(() => { copyMessage = ''; }, 1800);
    };
    try {
      if (navigator.clipboard?.writeText && window.isSecureContext) {
        await navigator.clipboard.writeText(value);
        return done('已复制。');
      }
      const input = document.createElement('textarea');
      input.value = value;
      input.setAttribute('readonly', 'true');
      input.style.position = 'fixed';
      input.style.opacity = '0';
      input.style.pointerEvents = 'none';
      document.body.appendChild(input);
      input.select();
      input.setSelectionRange(0, input.value.length);
      const copied = document.execCommand('copy');
      document.body.removeChild(input);
      if (copied) return done(window.isSecureContext ? '已复制。' : '已兼容复制。');
    } catch {
      // fall through to manual copy hint
    }
    done(window.isSecureContext ? '复制失败，请手动复制。' : '当前为 HTTP 环境，系统剪贴板受限，请手动复制。');
  }
  function formatBytes(value: number) { if (!value) return '0 B'; const units = ['B', 'KB', 'MB', 'GB', 'TB']; let size = value; let index = 0; while (size >= 1024 && index < units.length - 1) { size /= 1024; index += 1; } return `${size.toFixed(size >= 10 || index === 0 ? 0 : 1)} ${units[index]}`; }
  function recentTrafficTotal() { return homeStats.recentTraffic.reduce((sum, point) => sum + point.bytes, 0); }
  function trafficPercent(bytes: number) {
    const maxBytes = Math.max(0, ...homeStats.recentTraffic.map((point) => point.bytes));
    if (maxBytes <= 0 || bytes <= 0) return 0;
    return Math.max(4, Math.round((bytes / maxBytes) * 100));
  }
  async function readJSON(res: Response) { try { return await res.json(); } catch { return { error: res.ok ? '' : `请求失败：HTTP ${res.status}` }; } }
  function formatDateTime(value: string) { if (!value) return '无'; const date = new Date(value); if (Number.isNaN(date.getTime())) return value; return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')} ${String(date.getHours()).padStart(2, '0')}:${String(date.getMinutes()).padStart(2, '0')}`; }
  function pageRange() { if (resourceTotalPages <= 1) return []; const start = Math.max(1, resourcePage - 2); const end = Math.min(resourceTotalPages, resourcePage + 2); return Array.from({ length: end - start + 1 }, (_, index) => start + index); }
  function exploreTotalPages() { return Math.max(1, Math.ceil(featuredResources.length / exploreFeaturedPageSize)); }
  function explorePageRange() { const total = exploreTotalPages(); if (total <= 1) return []; const start = Math.max(1, explorePage - 2); const end = Math.min(total, explorePage + 2); return Array.from({ length: end - start + 1 }, (_, index) => start + index); }
  function exploreFeaturedResources() { const total = exploreTotalPages(); if (explorePage > total) explorePage = total; if (explorePage < 1) explorePage = 1; const start = (explorePage - 1) * exploreFeaturedPageSize; return featuredResources.slice(start, start + exploreFeaturedPageSize); }
  function changeExplorePage(page: number) { explorePage = Math.min(Math.max(1, page), exploreTotalPages()); window.scrollTo({ top: 0, behavior: 'smooth' }); }
  function groupLabel(value: string) { return groupLabels[value] ?? value; }
  function resourceTypeLabel(value: string) { return resourceTypeLabels[value] ?? value; }
  function dispositionLabel(value?: string) { return value === 'attachment' ? '强制下载' : '页面内打开'; }
  function translateSystemMessage(value = '') {
    if (!value) return '';
    if (value === 'allowed by policy') return '策略允许';
    if (value === 'upload denied by policy') return '策略禁止上传';
    if (value === 'access denied by policy') return '策略禁止访问';
    if (value === 'login required by policy') return '策略要求先登录';
    if (value.startsWith('file size exceeds policy limit')) return '文件大小超过策略上限';
    if (value === 'same IP daily upload limit exceeded') return '同一 IP 今日上传次数已达上限';
    if (value.startsWith('no policy rule matched')) return '没有匹配的策略规则';
    if (value.startsWith('unsupported policy action')) return '不支持的策略动作';
    return value;
  }
  function bytesToUnit(value: number, unit: number) { if (!value) return '0'; const converted = value / unit; return Number.isInteger(converted) ? String(converted) : converted.toFixed(2).replace(/\.?0+$/, ''); }
  function unitToBytes(raw: string, unit: number) { const normalized = Number(raw); if (!Number.isFinite(normalized) || normalized <= 0) return 0; return Math.round(normalized * unit); }
  function clampCompressionQuality(value: number) { const next = Number(value) || 50; return Math.max(50, Math.min(80, Math.round(next))); }
  function compressionHint(group?: UserGroup | null) { if (!group?.imageCompressionEnabled) return '图片压缩关闭'; return `图片压缩质量 ${clampCompressionQuality(group.imageCompressionQuality)}%`; }
  function updateRuleMaxFileSize(rule: PolicyRule, value: string) { rule.maxFileSizeBytes = unitToBytes(value, megabyte); matrixBaseRules = [...matrixBaseRules]; }
  function updateRuleMonthlyTraffic(rule: PolicyRule, value: string) { rule.monthlyTrafficPerResourceBytes = unitToBytes(value, gigabyte); matrixBaseRules = [...matrixBaseRules]; }
  function updateOverrideMaxFileSize(index: number, value: string) { matrixOverrideRules[index].maxFileSizeBytes = unitToBytes(value, megabyte); matrixOverrideRules = [...matrixOverrideRules]; }
  function updateOverrideMonthlyTraffic(index: number, value: string) { matrixOverrideRules[index].monthlyTrafficPerResourceBytes = unitToBytes(value, gigabyte); matrixOverrideRules = [...matrixOverrideRules]; }
  function resourceBadge(record: ResourceRecord) { return `${resourceTypeLabel(record.type)} · ${formatBytes(record.size)} · ${record.isPrivate ? '私有' : '公开'} · ${record.status === 'deleted' ? '已删除' : '正常'}`; }
  function securityHint(record: ResourceRecord) { if (record.isPrivate) return '私有资源默认拒绝匿名直链访问，可使用签名链接按时效开放。'; if (record.type === 'image') return '图片资源可直接预览。'; if (record.type === 'video') return '视频资源可在详情页内直接预览。'; if (record.type === 'script' || record.type === 'executable') return '脚本和可执行资源会强制下载，避免浏览器直接执行。'; return '非图片资源展示类型、大小、下载策略和安全提示。'; }
  function isFeaturedResource(resourceId: string) { return featuredResources.some((item) => item.resource.id === resourceId); }
  function homeFeaturedResources() { return featuredResources.slice(0, homeFeaturedLimit); }
  function featuredOverflowCount() { return Math.max(0, featuredResources.length - homeFeaturedLimit); }
  function openGalleryModal(record: ResourceRecord) { galleryModalResource = record; copyMessage = ''; }
  function closeGalleryModal() { galleryModalResource = null; }
  function monthlyUsageLabel(record: ResourceRecord) { return record.monthlyLimit > 0 ? `${formatBytes(record.monthlyTraffic)} / ${formatBytes(record.monthlyLimit)}` : `${formatBytes(record.monthlyTraffic)} / 不限`; }
  function quotaHint(group?: UserGroup | null) { if (!group) return '当前没有用户组配额信息。'; const parts: string[] = []; if (group.maxFileSizeBytes > 0) parts.push(`单文件 ${formatBytes(group.maxFileSizeBytes)}`); if (group.totalCapacityBytes > 0) parts.push(`总容量 ${formatBytes(group.totalCapacityBytes)}`); if (group.dailyUploadLimit > 0) parts.push(`每日 ${group.dailyUploadLimit} 次`); if (group.dailyIpUploadLimit > 0) parts.push(`同 IP 每日 ${group.dailyIpUploadLimit} 次`); if (group.defaultMonthlyTrafficBytes > 0) parts.push(`默认月流量 ${formatBytes(group.defaultMonthlyTrafficBytes)}`); return parts.length > 0 ? parts.join(' · ') : '当前用户组未设置额外配额。'; }
  function findStorageConfig(configs: StorageConfig[], id: string, type: string) { return configs.find((config) => config.id === id) ?? configs.find((config) => config.type === type); }
  function ensureStorageConfig(config: Partial<StorageConfig>, type: string): StorageConfig {
    return {
      id: config.id ?? `${type}-default`,
      type,
      name: config.name ?? (type === 'local' ? '本机存储' : type === 's3' ? 'S3 兼容存储' : 'WebDAV 存储'),
      endpoint: config.endpoint ?? '',
      region: config.region ?? '',
      bucket: config.bucket ?? '',
      accessKeyId: config.accessKeyId ?? '',
      secretAccessKey: config.secretAccessKey ?? '',
      username: config.username ?? '',
      password: config.password ?? '',
      publicBaseUrl: config.publicBaseUrl ?? '',
      basePath: config.basePath ?? '',
      usePathStyle: config.usePathStyle ?? true,
      isDefault: config.isDefault ?? false,
      createdAt: config.createdAt,
      updatedAt: config.updatedAt
    };
  }
</script>

{#if !bootstrapReady}
  <main class="page-shell narrow">
    <section class="glass-panel page-panel">
      <p class="eyebrow">马赫环静态托管</p>
      <h1>正在加载</h1>
      <p class="lead compact">正在读取站点状态和登录信息。</p>
    </section>
  </main>
{:else if path === '/install'}
  <main class="page-shell narrow">
    <a class="back-link" href="/" on:click|preventDefault={() => navigate('/')}>返回首页</a>
    <section class="glass-panel page-panel">
      <p class="eyebrow">马赫环静态托管</p>
      <h1>初始化</h1>
      {#if !isKnownUninitialized()}
        <p class="form-error">{installLoadError || '当前系统不是可初始化状态。为避免覆盖已有数据，初始化入口已锁定。'}</p>
        <div class="actions"><a class="button secondary" href="/" on:click|preventDefault={() => navigate('/')}>返回首页</a><a class="button ghost" href="/login">去登录</a></div>
      {:else}
        <form class="resource-form" on:submit|preventDefault={initializeSite}>
          <label>站点名称<input bind:value={installForm.siteName} /></label>
          <label>默认存储<select bind:value={installForm.defaultStorage}><option value="local">本机存储</option></select></label>
          <label>管理员账号<input bind:value={installForm.adminUsername} autocomplete="username" /></label>
          <label>昵称<input bind:value={installForm.displayName} /></label>
          <label>密码<input bind:value={installForm.password} type="password" autocomplete="new-password" /></label>
          <label>确认密码<input bind:value={installForm.confirmPassword} type="password" autocomplete="new-password" /></label>
          {#if installError || installLoadError}<p class="form-error">{installError || installLoadError}</p>{/if}
          <button class="button primary" type="submit" disabled={isInitializing || !installReady}>{isInitializing ? '初始化中' : '创建管理员'}</button>
        </form>
      {/if}
    </section>
  </main>
{:else if path === '/upload'}
  <main class="page-shell wide" on:paste={handlePaste}>
    <a class="back-link" href="/" on:click|preventDefault={() => navigate('/')}>返回首页</a>
    <section class="glass-panel page-panel upload-panel">
      <div class="panel-head">
        <div>
          <p class="eyebrow">马赫环静态托管</p>
          <h1>上传</h1>
          <p class="lead compact">支持拖拽、点击选择、粘贴与批量上传。每个文件都会返回独立结果与全部外链格式。</p>
        </div>
        <div class="summary-card"><span>今日上传</span><strong>{homeStats.todayUploads}</strong></div>
      </div>
      {#if installLoadError}
        <p class="form-error">无法确认站点初始化状态：{installLoadError}</p>
      {:else if isKnownUninitialized()}
        <p class="lead compact">请先完成管理员初始化，再开放上传入口。</p>
        <a class="button primary" href="/install">去初始化</a>
      {:else}
        <form class="resource-form" on:submit|preventDefault={uploadSelectedResources}>
          {#if !currentUser && !siteSettings.allowGuestUploads}
            <p class="form-error">站点当前已关闭游客上传，请先登录后再上传。</p>
          {/if}
          <div class="summary-grid">
            <article class="summary-card">
              <span>当前身份</span>
              <strong>{currentUser ? currentUser.displayName : '游客'}</strong>
              <small>{accountUsage?.group?.name || uploadGroup}</small>
            </article>
            <article class="summary-card">
              <span>可用策略提示</span>
              <strong>{quotaHint(accountUsage?.group)}</strong>
              <small>{accountUsage ? `已用 ${formatBytes(accountUsage.usedStorageBytes)} · 今日 ${accountUsage.dailyUploadCount} 次 · ${compressionHint(accountUsage.group)}` : '加载中'}</small>
            </article>
          </div>
          {#if uploadDeliveryRoutes.length > 0}
            <label class="route-select-card">
              <span>访问线路</span>
              <select bind:value={selectedDeliveryRouteId} disabled={!allowDeliveryRouteSelection || uploadDeliveryRoutes.length <= 1}>
                {#each uploadDeliveryRoutes as route}
                  <option value={route.id}>{route.name}{route.publicBaseUrl ? ` · ${route.publicBaseUrl}` : ' · 默认站点地址'}</option>
                {/each}
              </select>
              <small>{allowDeliveryRouteSelection ? '本次上传返回所选线路的直链。' : '当前策略组由管理员固定访问线路。'}</small>
            </label>
          {/if}
          {#if accountError}<p class="form-error">{accountError}</p>{/if}
          <input id="file-picker" class="sr-only" type="file" multiple on:change={handleUploadSelection} />
          <div class:dragging={isDragging} class="drop-zone" role="button" tabindex="0" aria-label="资源上传拖拽区域" on:keydown={(event) => { if (event.key === 'Enter' || event.key === ' ') document.getElementById('file-picker')?.click(); }} on:dragenter|preventDefault={() => isDragging = true} on:dragover|preventDefault={() => isDragging = true} on:dragleave|preventDefault={() => isDragging = false} on:drop={handleDrop}>
            <p>把文件拖到这里，或点击按钮选择文件。</p>
            <div class="drop-actions"><label class="button secondary" for="file-picker">选择文件</label><span>也可以直接粘贴截图或文件。</span></div>
          </div>
          {#if uploadFiles.length > 0}
            <div class="upload-queue">
              {#each uploadQueue as item, index}
                <article class="upload-card">
                  <div class="upload-card-head"><div><strong>{item.name}</strong><span>{formatBytes(item.size)}</span></div>{#if item.status === 'pending'}<button class="button ghost compact" type="button" on:click={() => removeQueuedFile(index)}>移除</button>{/if}</div>
                  <div class="progress-track"><span style={`width: ${Math.round(item.progress * 100)}%`}></span></div>
                  <p class:success-copy={item.status === 'success'} class:error-copy={item.status === 'error'} class="upload-message">{item.message || (item.status === 'uploading' ? '上传中' : '等待上传')}</p>
                  {#if item.links}
                    <div class="upload-links-preview">
                      <label>直链<input readonly value={item.links.direct} /></label>
                    </div>
                    <div class="link-grid compact">
                      <button class="button ghost compact" type="button" on:click={() => copyText(item.links?.direct || '')}>复制直链</button>
                      <button class="button ghost compact" type="button" on:click={() => copyText(item.links?.markdown || '')}>复制 Markdown 链接</button>
                      <button class="button ghost compact" type="button" on:click={() => copyText(item.links?.html || '')}>复制 HTML 代码</button>
                      <button class="button ghost compact" type="button" on:click={() => copyText(item.links?.bbcode || '')}>复制 BBCode 代码</button>
                    </div>
                  {/if}
                </article>
              {/each}
            </div>
          {/if}
          {#if uploadError}<p class="form-error">{uploadError}</p>{/if}
          {#if copyMessage}<p class="form-success">{copyMessage}</p>{/if}
          {#if isUploading}<div class="overall-progress"><div class="progress-track"><span style={`width: ${Math.round(uploadProgress * 100)}%`}></span></div><span>{Math.round(uploadProgress * 100)}% · {formatUploadSpeed(uploadSpeedBps)}</span></div>{/if}
          <button class="button primary" type="submit" disabled={isUploading || uploadFiles.length === 0 || (!currentUser && !siteSettings.allowGuestUploads)}>{isUploading ? '上传中' : '开始上传'}</button>
        </form>
      {/if}
    </section>
  </main>
{:else if isAccountPage}
  <main class="page-shell wide">
    <a class="back-link" href="/" on:click|preventDefault={() => navigate('/')}>返回首页</a>
    <section class="glass-panel page-panel">
      <div class="panel-head">
        <div>
          <p class="eyebrow">账户中心</p>
          <h1>账户用量</h1>
          <p class="lead compact">查看当前账户所属用户组、存储占用、月流量和每日上传次数。</p>
        </div>
        <div class="summary-card"><span>当前用户组</span><strong>{accountUsage?.group?.name || '加载中'}</strong></div>
      </div>
      {#if !currentUser}
        <p class="lead compact">登录后可查看个人用量和当前配额。</p>
        <a class="button primary" href="/login">去登录</a>
      {:else}
        {#if accountError}<p class="form-error">{accountError}</p>{/if}
        {#if accountUsage}
          <div class="stats-grid">
            <article><span>存储占用</span><strong>{formatBytes(accountUsage.usedStorageBytes)}</strong></article>
            <article><span>本月流量</span><strong>{formatBytes(accountUsage.monthlyTrafficBytes)}</strong></article>
            <article><span>今日上传</span><strong>{accountUsage.dailyUploadCount}</strong></article>
            <article><span>当前配额</span><strong>{quotaHint(accountUsage.group)}</strong></article>
          </div>
          <dl class="detail-list">
            <div><dt>昵称</dt><dd>{currentUser.displayName}</dd></div>
            <div><dt>账号</dt><dd>{currentUser.username}</dd></div>
            <div><dt>用户组</dt><dd>{accountUsage.group.name}</dd></div>
            <div><dt>外链权限</dt><dd>{accountUsage.group.allowHotlink ? '允许' : '禁止匿名外链'}</dd></div>
          </dl>
        <div class="actions">
          <a class="button primary" href="/upload">继续上传</a>
            {#if currentUser.role === 'admin'}<a class="button secondary" href="/admin" on:click|preventDefault={() => navigate('/admin')}>进入后台</a>{/if}
        </div>
        {/if}
      {/if}
    </section>
  </main>
{:else if path === '/login'}
  <main class="page-shell narrow">
    <a class="back-link" href="/" on:click|preventDefault={() => navigate('/')}>返回首页</a>
    <section class="glass-panel page-panel">
      <p class="eyebrow">马赫环静态托管</p>
      <h1>登录</h1>
      <form class="resource-form" on:submit|preventDefault={login}>
        <label>账号<input bind:value={loginForm.username} autocomplete="username" /></label>
        <label>密码<input bind:value={loginForm.password} type="password" autocomplete="current-password" /></label>
        {#if loginError}<p class="form-error">{loginError}</p>{/if}
        <button class="button primary" type="submit" disabled={isLoggingIn || !installState?.initialized}>{isLoggingIn ? '登录中' : '登录'}</button>
      </form>
    </section>
  </main>
{:else if isResourceDetailPage}
  {#if !authReady}
  <main class="page-shell narrow">
    <section class="glass-panel page-panel">
      <p class="eyebrow">资源详情</p>
      <h1>正在检查登录状态</h1>
      <p class="lead compact">正在确认管理员会话。</p>
    </section>
  </main>
  {:else if !currentUser || currentUser.role !== 'admin'}
  <main class="page-shell narrow">
    <section class="glass-panel page-panel">
      <p class="eyebrow">资源详情</p>
      <h1>需要管理员登录</h1>
      <p class="lead compact">正在跳转到登录页。</p>
    </section>
  </main>
  {:else}
  <main class="page-shell wide">
    <a class="back-link" href="/admin/resources" on:click|preventDefault={() => navigate('/admin/resources')}>返回资源管理</a>
    <section class="glass-panel page-panel resource-detail-page">
      <div class="panel-head">
        <div>
          <p class="eyebrow">资源详情</p>
          <h1>资源详情</h1>
          <p class="lead compact">独立查看资源元数据、流量、外链和访问窗口。</p>
        </div>
        <div class="summary-card"><span>当前账号</span><strong>{currentUser.displayName}</strong></div>
      </div>
      <div class="resource-detail-toolbar">
        <a class="button ghost compact" href="/admin" on:click|preventDefault={() => navigate('/admin')}>返回后台首页</a>
        <a class="button secondary compact" href="/admin/resources" on:click|preventDefault={() => navigate('/admin/resources')}>返回资源列表</a>
        {#if resourceDetail && copyMessage}<span class="form-success">{copyMessage}</span>{/if}
      </div>
      {#if detailError}
        <p class="form-error">{detailError}</p>
      {:else if isLoadingDetail || !resourceDetail}
        <p>加载详情中…</p>
      {:else}
        <div class="resource-detail-grid">
          <section class="detail-panel">
            <div class="subsection-heading">
              <h3>{resourceDetail.record.originalName}</h3>
              <p>{securityHint(resourceDetail.record)}</p>
            </div>
            {#if resourceDetail.record.type === 'image' && resourceDetail.record.status !== 'deleted'}
              <div class="preview-panel">
                <img src={resourceDetail.record.publicUrl} alt={resourceDetail.record.originalName} />
              </div>
            {:else}
              <div class="preview-panel muted">
                <strong>{resourceDetail.record.type}</strong>
                <span>{formatBytes(resourceDetail.record.size)}</span>
              </div>
            {/if}
            <div class="resource-actions">
              <button class="button secondary compact" type="button" on:click={() => updateResourceVisibility(resourceDetail.record.id, !resourceDetail.record.isPrivate)}>
                {resourceDetail.record.isPrivate ? '设为公开' : '设为私有'}
              </button>
            </div>
            <div class="link-grid">
              <label>直链<input readonly value={resourceDetail.links.direct} /></label>
              <button class="button ghost compact" type="button" on:click={() => copyText(resourceDetail.links.direct)}>复制直链</button>
              <label>Markdown 链接<input readonly value={resourceDetail.links.markdown} /></label>
              <button class="button ghost compact" type="button" on:click={() => copyText(resourceDetail.links.markdown)}>复制 Markdown 链接</button>
              <label>HTML 代码<input readonly value={resourceDetail.links.html} /></label>
              <button class="button ghost compact" type="button" on:click={() => copyText(resourceDetail.links.html)}>复制 HTML 代码</button>
              <label>BBCode 代码<input readonly value={resourceDetail.links.bbcode} /></label>
              <button class="button ghost compact" type="button" on:click={() => copyText(resourceDetail.links.bbcode)}>复制 BBCode 代码</button>
              <label>签名链接有效期（秒）<input bind:value={signedLinkExpiresInSeconds} min="60" max="604800" type="number" /></label>
              <button class="button ghost compact" type="button" on:click={() => generateSignedLink(resourceDetail.record.id)}>生成签名链接</button>
              <label>签名链接<input readonly value={signedLinkResult?.url ?? ''} /></label>
              <button class="button ghost compact" disabled={!signedLinkResult?.url} type="button" on:click={() => copyText(signedLinkResult?.url ?? '')}>复制签名链接</button>
              <label>签名过期时间<input readonly value={signedLinkResult?.expiresAt ? formatDateTime(signedLinkResult.expiresAt) : ''} /></label>
            </div>
          </section>
          <section class="detail-panel">
            <div class="stats-grid">
              <article><span>访问次数</span><strong>{resourceDetail.record.accessCount}</strong></article>
              <article><span>累计流量</span><strong>{formatBytes(resourceDetail.record.trafficBytes)}</strong></article>
              <article><span>月流量</span><strong>{formatBytes(resourceDetail.record.monthlyTraffic)} / {formatBytes(resourceDetail.record.monthlyLimit)}</strong></article>
              <article><span>图片尺寸</span><strong>{resourceDetail.metadata.imageWidth > 0 ? `${resourceDetail.metadata.imageWidth} × ${resourceDetail.metadata.imageHeight}` : '无'}</strong></article>
            </div>
            <dl class="detail-list">
              <div><dt>资源 ID</dt><dd>{resourceDetail.record.id}</dd></div>
              <div><dt>可见性</dt><dd>{resourceDetail.record.isPrivate ? '私有' : '公开'}</dd></div>
              <div><dt>存储驱动</dt><dd>{resourceDetail.record.storageDriver}</dd></div>
              <div><dt>对象键</dt><dd>{resourceDetail.record.objectKey}</dd></div>
              <div><dt>MIME</dt><dd>{resourceDetail.record.contentType || '未知'}</dd></div>
              <div><dt>缓存策略</dt><dd>{resourceDetail.record.cacheControl || '未设置'}</dd></div>
              <div><dt>下载策略</dt><dd>{dispositionLabel(resourceDetail.record.disposition)}</dd></div>
              <div><dt>上传 IP</dt><dd>{resourceDetail.record.uploadIp || '无'}</dd></div>
              <div><dt>User-Agent</dt><dd>{resourceDetail.record.uploadUserAgent || '无'}</dd></div>
              <div><dt>头摘要</dt><dd>{resourceDetail.metadata.headerSha256 || '无'}</dd></div>
              <div><dt>创建时间</dt><dd>{formatDateTime(resourceDetail.record.createdAt)}</dd></div>
            </dl>
            <div class="subsection-heading compact-head"><h3>流量窗口</h3></div>
            <div class="window-list">
              {#each resourceDetail.trafficWindows as window}
                <article class="window-card">
                  <strong>{window.windowType === 'month' ? '月窗口' : '日窗口'} {window.windowKey}</strong>
                  <span>{window.requestCount} 次访问 · {formatBytes(window.trafficBytes)}</span>
                </article>
              {/each}
              {#if resourceDetail.trafficWindows.length === 0}
                <p>还没有访问记录。</p>
              {/if}
            </div>
          </section>
        </div>
      {/if}
    </section>
  </main>
  {/if}
{:else if isAdminPage}
  {#if !authReady}
  <main class="page-shell narrow">
    <section class="glass-panel page-panel">
      <p class="eyebrow">后台管理</p>
      <h1>正在检查登录状态</h1>
      <p class="lead compact">正在确认管理员会话。</p>
    </section>
  </main>
  {:else if !currentUser}
  <main class="page-shell narrow">
    <section class="glass-panel page-panel">
      <p class="eyebrow">后台管理</p>
      <h1>需要管理员登录</h1>
      <p class="lead compact">正在跳转到登录页。</p>
    </section>
  </main>
  {:else}
  <main class="admin-shell">
    <aside class="admin-sidebar glass-panel" aria-label="后台导航">
      <a class="back-link" href="/" on:click|preventDefault={() => navigate('/')}>返回首页</a>
      <p class="eyebrow">后台管理</p>
      <h1>后台</h1>
      <nav class="admin-nav" aria-label="后台功能">
        <a class:active={isDashboardPage} class="admin-nav-link" href="/admin" on:click|preventDefault={() => navigate('/admin')}>仪表盘</a>
        <a class:active={isPolicyPage} class="admin-nav-link" href="/admin/policies" on:click|preventDefault={() => navigate('/admin/policies')}>策略组</a>
        <a class:active={isUserGroupPage} class="admin-nav-link" href="/admin/user-groups" on:click|preventDefault={() => navigate('/admin/user-groups')}>用户组</a>
        <a class:active={isUserPage} class="admin-nav-link" href="/admin/users" on:click|preventDefault={() => navigate('/admin/users')}>用户管理</a>
        <a class:active={isStoragePage} class="admin-nav-link" href="/admin/storage" on:click|preventDefault={() => navigate('/admin/storage')}>存储设置</a>
        <a class:active={isSiteSettingsPage} class="admin-nav-link" href="/admin/site" on:click|preventDefault={() => navigate('/admin/site')}>站点设置</a>
        <a class:active={isCompressionPage} class="admin-nav-link" href="/admin/compression" on:click|preventDefault={() => navigate('/admin/compression')}>图片压缩</a>
        <a class:active={isFeaturedAdminPage} class="admin-nav-link" href="/admin/featured" on:click|preventDefault={() => navigate('/admin/featured')}>精选管理</a>
        <a class:active={isResourcePage || isResourceDetailPage} class="admin-nav-link" href="/admin/resources" on:click|preventDefault={() => navigate('/admin/resources')}>资源管理</a>
      </nav>
      <div class="sidebar-meta"><p>{currentUser.displayName}</p><span>{currentUser.groupName}</span></div><button class="button secondary" type="button" on:click={logout}>退出登录</button>
    </aside>
    <section class="admin-workspace glass-panel">
      {#if isDashboardPage}
        <div class="section-heading"><p class="eyebrow">后台仪表盘</p><h2>仪表盘</h2><p>汇总站点资源、用户和今日活动，作为后台的核心统计入口。</p></div>
        <div class="admin-session">{#if currentUser}<span>当前账号：{currentUser.displayName} / {currentUser.groupName}</span>{:else if authReady}<span>需要管理员登录。</span><a class="inline-link" href="/login">去登录</a>{:else}<span>正在检查登录状态。</span>{/if}</div>
        <div class="stats-grid dashboard-grid">
          <article><span>资源总数</span><strong>{homeStats.totalResources}</strong></article>
          <article><span>有效资源</span><strong>{homeStats.activeResources}</strong></article>
          <article><span>累计存储</span><strong>{formatBytes(homeStats.totalStorageBytes)}</strong></article>
          <article><span>累计流量</span><strong>{formatBytes(homeStats.totalTrafficBytes)}</strong></article>
          <article><span>今日上传</span><strong>{homeStats.todayUploads}</strong></article>
          <article><span>用户总数</span><strong>{managedUsers.length}</strong></article>
          <article><span>用户组数量</span><strong>{userGroups.length}</strong></article>
          <article><span>近七日流量</span><strong>{formatBytes(recentTrafficTotal())}</strong></article>
        </div>
        <section class="traffic-table-panel">
          <div class="subsection-heading"><h3>近七日流量明细</h3><p>按日期汇总真实传输字节，分片下载只累计实际传出的范围字节。</p></div>
          <div class="traffic-table-wrap">
            <table class="traffic-table">
              <thead><tr><th>日期</th><th>流量</th><th>占比</th></tr></thead>
              <tbody>
                {#each homeStats.recentTraffic as point}
                  <tr>
                    <td>{point.label}</td>
                    <td>{formatBytes(point.bytes)}</td>
                    <td><div class="traffic-bar" aria-label={`流量占比 ${trafficPercent(point.bytes)}%`}><span style={`width: ${trafficPercent(point.bytes)}%`}></span></div></td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
        </section>
      {:else if isPolicyPage}
        <div class="section-heading"><p class="eyebrow">策略组管理</p><h2>策略组</h2><p>复制、启用、停用不同策略组，并对选中策略组直接编辑规则。</p></div>
        <div class="admin-session"><span>当前账号：{currentUser.displayName} / {currentUser.groupName}</span></div>
        <div class="policy-groups-layout">
          <section class="policy-groups-panel">
            <div class="subsection-heading"><h3>策略组列表</h3><p>上传和访问始终使用当前启用的策略组。</p></div>
            <form class="policy-group-create" on:submit|preventDefault={createPolicyGroup}>
              <label>名称<input bind:value={policyGroupForm.name} placeholder="新策略组" /></label>
              <label>说明<input bind:value={policyGroupForm.description} placeholder="可选说明" /></label>
              <button class="button secondary" type="submit" disabled={isCreatingPolicyGroup || !currentUser}>{isCreatingPolicyGroup ? '创建中' : '新建策略组'}</button>
            </form>
            {#if policyGroupError}<p class="form-error">{policyGroupError}</p>{/if}
            <div class="policy-group-list">{#each policyGroups as item}<article class:selected={item.id === selectedPolicyGroupId} class="policy-group-card"><button class="policy-group-select" type="button" on:click={() => loadPolicyEditor(item.id)}><strong>{item.name}</strong><span>{item.isActive ? '已启用' : '未启用'}{item.isDefault ? ' · 默认' : ''}</span></button><p>{item.description || '暂无说明'}</p><div class="policy-group-actions"><button class="button secondary compact" type="button" on:click={() => copyPolicyGroup(item)} disabled={!currentUser}>复制</button>{#if item.isActive}<button class="button secondary compact" type="button" on:click={() => setPolicyGroupActive(item, false)} disabled={!currentUser}>停用</button>{:else}<button class="button primary compact" type="button" on:click={() => setPolicyGroupActive(item, true)} disabled={!currentUser}>启用</button>{/if}</div></article>{/each}</div>
            {#if selectedPolicyGroupId}
              {#each policyGroups.filter((group) => group.id === selectedPolicyGroupId) as selectedGroup}
                <div class="route-policy-card">
                  <div class="subsection-heading"><h3>策略组访问线路</h3><p>上传时按这里决定默认直链线路，并控制是否允许上传者选择。</p></div>
                  <label>默认线路<select bind:value={selectedGroup.defaultDeliveryRouteId}>{#each deliveryRoutes as route}<option value={route.id}>{route.name}</option>{/each}</select></label>
                  <label class="toggle-row"><span>允许上传者选择线路</span><input bind:checked={selectedGroup.allowDeliveryRouteSelection} type="checkbox" /></label>
                  <div class="route-checkboxes">
                    {#each deliveryRoutes as route}
                      <label><input type="checkbox" checked={policyGroupAllowedRouteIds(selectedGroup).includes(route.id)} on:change={() => togglePolicyGroupRoute(selectedGroup, route.id)} /> <span>{route.name}</span></label>
                    {/each}
                  </div>
                  <button class="button primary compact" type="button" on:click={() => savePolicyGroupSettings(selectedGroup)}>保存线路策略</button>
                </div>
              {/each}
            {/if}
            <div class="route-policy-card">
              <div class="subsection-heading"><h3>访问线路</h3><p>可自定义任意反代入口，例如大陆优化、下载专线或备用节点。</p></div>
              {#if deliveryRouteError}<p class="form-error">{deliveryRouteError}</p>{:else if deliveryRouteMessage}<p class="form-success">{deliveryRouteMessage}</p>{/if}
              <button class="button secondary compact" type="button" on:click={addDeliveryRoute}>新增线路</button>
              <div class="route-config-list">
                {#each deliveryRoutes as route}
                  <article class="route-config-card">
                    <label>ID<input bind:value={route.id} disabled={!!route.createdAt} /></label>
                    <label>名称<input bind:value={route.name} /></label>
                    <label>入口地址<input bind:value={route.publicBaseUrl} placeholder="https://cdn.example.com，留空使用默认站点地址" /></label>
                    <label>说明<input bind:value={route.description} /></label>
                    <label class="toggle-row"><span>启用</span><input bind:checked={route.isEnabled} type="checkbox" /></label>
                    <label class="toggle-row"><span>默认线路</span><input bind:checked={route.isDefault} type="checkbox" /></label>
                    <button class="button primary compact" type="button" on:click={() => saveDeliveryRoute(route)} disabled={savingDeliveryRouteId === route.id}>{savingDeliveryRouteId === route.id ? '保存中' : '保存线路'}</button>
                  </article>
                {/each}
              </div>
            </div>
          </section>
          <section class="policy-editor-panel">
            <div class="subsection-heading"><h3>规则编辑</h3><p>当前编辑：{policyGroups.find((group) => group.id === selectedPolicyGroupId)?.name ?? '未选择策略组'}</p></div>
            <div class="policy-editor-toolbar">
              <div class="policy-editor-meta">
                <strong>{policyGroups.find((group) => group.id === selectedPolicyGroupId)?.name ?? '未选择策略组'}</strong>
                <span>基础规则 {matrixBaseRules.length} 条 · 覆盖规则 {matrixOverrideRules.length} 条</span>
              </div>
              <div class="policy-editor-actions">
                <button class="button ghost compact" type="button" disabled={!selectedPolicyGroupId} on:click={() => loadPolicyEditor(selectedPolicyGroupId)}>刷新</button>
                <button class="button primary" type="button" disabled={isSavingPolicies || !currentUser || !selectedPolicyGroupId} on:click={savePolicies}>{isSavingPolicies ? '保存中' : '保存策略'}</button>
              </div>
            </div>
            <div class="matrix-block"><table class="policy-matrix-table"><thead><tr><th>用户组</th><th>资源类型</th><th>允许上传</th><th>允许访问</th><th>单文件上限 (MB)</th><th>单资源月流量上限 (GB)</th><th>下载方式</th><th>缓存策略</th></tr></thead><tbody>{#each matrixBaseRules as rule, index}<tr><td>{groupLabel(rule.userGroup)}</td><td>{resourceTypeLabel(rule.resourceType)}</td><td><input aria-label={`allow upload ${index}`} type="checkbox" bind:checked={rule.allowUpload} /></td><td><input aria-label={`allow access ${index}`} type="checkbox" bind:checked={rule.allowAccess} /></td><td><input aria-label={`max file ${index}`} type="number" min="0" step="0.1" value={bytesToUnit(rule.maxFileSizeBytes, megabyte)} on:input={(event) => updateRuleMaxFileSize(rule, (event.currentTarget as HTMLInputElement).value)} /></td><td><input aria-label={`monthly traffic ${index}`} type="number" min="0" step="0.1" value={bytesToUnit(rule.monthlyTrafficPerResourceBytes, gigabyte)} on:input={(event) => updateRuleMonthlyTraffic(rule, (event.currentTarget as HTMLInputElement).value)} /></td><td><select aria-label={`disposition ${index}`} bind:value={rule.downloadDisposition}><option value="">页面内打开</option><option value="attachment">强制下载</option></select></td><td><input aria-label={`cache ${index}`} bind:value={rule.cacheControl} placeholder="例如：public, max-age=31536000" /></td></tr>{/each}</tbody></table></div>
            <div class="override-panel"><div class="subsection-heading"><h3>扩展名覆盖</h3><p>用于按扩展名覆盖同类资源的默认规则。</p></div><button class="button secondary compact" type="button" on:click={addOverrideRule}>添加覆盖规则</button><div class="override-list">{#each matrixOverrideRules as rule, index}<div class="override-row"><select aria-label={`override group ${index}`} bind:value={rule.userGroup}>{#each groupOptions as option}<option value={option}>{groupLabel(option)}</option>{/each}</select><select aria-label={`override type ${index}`} bind:value={rule.resourceType}>{#each resourceTypeOptions as option}<option value={option}>{resourceTypeLabel(option)}</option>{/each}</select><input aria-label={`override ext ${index}`} bind:value={rule.extension} placeholder="例如：jpg" /><label><span>上传</span><input type="checkbox" bind:checked={rule.allowUpload} /></label><label><span>访问</span><input type="checkbox" bind:checked={rule.allowAccess} /></label><input aria-label={`override max ${index}`} type="number" min="0" step="0.1" value={bytesToUnit(rule.maxFileSizeBytes, megabyte)} on:input={(event) => updateOverrideMaxFileSize(index, (event.currentTarget as HTMLInputElement).value)} /><input aria-label={`override monthly ${index}`} type="number" min="0" step="0.1" value={bytesToUnit(rule.monthlyTrafficPerResourceBytes, gigabyte)} on:input={(event) => updateOverrideMonthlyTraffic(index, (event.currentTarget as HTMLInputElement).value)} /><select aria-label={`override disposition ${index}`} bind:value={rule.downloadDisposition}><option value="">页面内打开</option><option value="attachment">强制下载</option></select><button class="button ghost compact" type="button" on:click={() => removeOverrideRule(index)}>移除</button></div>{/each}</div></div>
            {#if matrixError}<p class="form-error">{matrixError}</p>{/if}
            {#if policySaveError}<p class="form-error">{policySaveError}</p>{:else if policySaveMessage}<p class="form-success">{policySaveMessage}</p>{/if}
            <details class="policy-json-details"><summary>高级 JSON</summary><textarea bind:value={rulesJson} rows="12" spellcheck="false"></textarea><button class="button secondary compact" type="button" on:click={applyAdvancedJson}>应用 JSON</button></details>
          </section>
        </div>
        <div class="policy-test-section"><div class="subsection-heading"><h3>策略测试</h3><p>测试结果会显示当前命中的启用策略组。</p></div><form class="policy-test-form" on:submit|preventDefault={runPolicyTest}><label>动作<select bind:value={policyForm.action}><option value="upload">上传</option><option value="access">访问</option></select></label><label>用户组<select bind:value={policyForm.group}><option value="guest">游客</option><option value="user">登录用户</option><option value="admin">管理员</option></select></label><label>文件名<input bind:value={policyForm.filename} placeholder="demo.jpg" /></label><label>MIME<input bind:value={policyForm.contentType} placeholder="image/jpeg" /></label><label>文件大小 (MB)<input bind:value={policyForm.size} min="0" step="0.1" type="number" /></label><button class="button primary" type="submit" disabled={isTestingPolicy || !currentUser}>{isTestingPolicy ? '测试中' : '测试策略'}</button></form><div class="policy-outcome" aria-live="polite">{#if policyError}<p class="result-state denied">测试失败</p><p>{policyError}</p>{:else if policyResult}<p class:allowed={policyResult.decision.allowed} class:denied={!policyResult.decision.allowed} class="result-state">{policyResult.decision.allowed ? '允许' : '拒绝'}</p><dl class="result-list"><div><dt>命中策略组</dt><dd>{policyResult.policyGroup.name}</dd></div><div><dt>原因</dt><dd>{policyResult.decision.reason}</dd></div><div><dt>资源类型</dt><dd>{resourceTypeLabel(policyResult.metadata.type)}</dd></div><div><dt>扩展名</dt><dd>{policyResult.metadata.extension || '无'}</dd></div><div><dt>命中用户组</dt><dd>{groupLabel(policyResult.decision.rule.userGroup || '') || '无'}</dd></div><div><dt>单文件限制</dt><dd>{formatBytes(policyResult.decision.rule.maxFileSizeBytes)}</dd></div><div><dt>单资源月流量</dt><dd>{formatBytes(policyResult.decision.rule.monthlyTrafficPerResourceBytes)}</dd></div><div><dt>下载策略</dt><dd>{dispositionLabel(policyResult.decision.rule.downloadDisposition)}</dd></div></dl>{:else}<p class="result-state">等待测试</p><p>示例默认按游客上传 1 MB JPG 资源。</p>{/if}</div></div>
      {:else if isUserGroupPage}
        <div class="section-heading"><p class="eyebrow">User Groups</p><h2>用户组与配额</h2><p>统一设置游客、普通用户、管理员的容量、单文件、默认月流量、每日上传次数和外链权限。</p></div>
        <div class="admin-session">{#if currentUser}<span>当前账号：{currentUser.displayName} / {currentUser.groupName}</span>{:else if authReady}<span>需要管理员登录。</span><a class="inline-link" href="/login">去登录</a>{:else}<span>正在检查登录状态。</span>{/if}</div>
        {#if userGroupError}<p class="form-error">{userGroupError}</p>{:else if userGroupMessage}<p class="form-success">{userGroupMessage}</p>{/if}
        <div class="resource-list">
          {#each userGroups as group}
            <article class="detail-panel">
              <div class="subsection-heading"><h3>{group.name}</h3><p>{group.description || '暂无说明'}</p></div>
              <div class="resource-filter-grid">
                <label>名称<input bind:value={group.name} /></label>
                <label>说明<input bind:value={group.description} /></label>
                <label>总容量<input bind:value={group.totalCapacityBytes} type="number" min="0" /></label>
                <label>默认月流量<input bind:value={group.defaultMonthlyTrafficBytes} type="number" min="0" /></label>
                <label>单文件限制<input bind:value={group.maxFileSizeBytes} type="number" min="0" /></label>
                <label>每日上传次数<input bind:value={group.dailyUploadLimit} type="number" min="0" /></label>
                <label>同 IP 每日上传<input bind:value={group.dailyIpUploadLimit} type="number" min="0" /></label>
              </div>
              <p class="muted-copy">同 IP 每日上传用于防盗刷，0 表示不限制；游客默认 5 次。</p>
              <label class="toggle-row"><span>允许匿名外链</span><input bind:checked={group.allowHotlink} type="checkbox" /></label>
              <div class="resource-actions"><button class="button primary compact" type="button" on:click={() => saveUserGroup(group)} disabled={savingUserGroupId === group.id}>{savingUserGroupId === group.id ? '保存中' : '保存用户组'}</button></div>
            </article>
          {/each}
        </div>
      {:else if isUserPage}
        <div class="section-heading"><p class="eyebrow">用户管理</p><h2>用户管理</h2><p>管理员可以创建用户、编辑昵称、状态、用户组，并对封禁与重置密码进行二次确认。</p></div>
        <div class="admin-session">{#if currentUser}<span>当前账号：{currentUser.displayName} / {currentUser.groupName}</span>{:else if authReady}<span>需要管理员登录。</span><a class="inline-link" href="/login">去登录</a>{:else}<span>正在检查登录状态。</span>{/if}</div>
        <form class="resource-filter-grid" on:submit|preventDefault={createManagedUser}>
          <label>账号<input bind:value={createUserForm.username} /></label>
          <label>昵称<input bind:value={createUserForm.displayName} /></label>
          <label>初始密码<input bind:value={createUserForm.password} type="password" /></label>
          <label>用户组<select bind:value={createUserForm.groupId}>{#each userGroups as group}<option value={group.id}>{group.name}</option>{/each}</select></label>
          <label>状态<select bind:value={createUserForm.status}><option value="active">正常</option><option value="disabled">停用</option><option value="banned">封禁</option></select></label>
          <button class="button primary filter-submit" type="submit" disabled={isCreatingUser}>{isCreatingUser ? '创建中' : '创建用户'}</button>
        </form>
        {#if userAdminError}<p class="form-error">{userAdminError}</p>{:else if userAdminMessage}<p class="form-success">{userAdminMessage}</p>{/if}
        <div class="resource-list">
          {#each managedUsers as user}
            <article class="resource-row user-row">
              <div class="resource-main">
                <div class="resource-title"><h3>{user.username}</h3><span>{user.role} · {user.id}</span></div>
                <p>当前用户组：{user.groupName}</p>
              </div>
              <div class="user-edit-grid">
                <label>昵称<input bind:value={user.displayName} /></label>
                <label>用户组<select bind:value={user.groupId}>{#each userGroups as group}<option value={group.id}>{group.name}</option>{/each}</select></label>
                <label>状态<select bind:value={user.status}><option value="active">正常</option><option value="disabled">停用</option><option value="banned">封禁</option></select></label>
              </div>
              <div class="resource-actions">
                <button class="button primary compact" type="button" on:click={() => saveManagedUser(user)} disabled={savingManagedUserId === user.id}>{savingManagedUserId === user.id ? '保存中' : '保存'}</button>
                <button class="button secondary compact" type="button" on:click={() => toggleUserBan(user)}>{user.status === 'banned' ? '解封' : '封禁'}</button>
                <button class="button ghost compact" type="button" on:click={() => resetManagedUserPassword(user)}>重置密码</button>
              </div>
            </article>
          {/each}
        </div>
      {:else if isStoragePage}
        <div class="section-heading"><p class="eyebrow">存储配置</p><h2>存储设置</h2><p>配置本机、S3 兼容和 WebDAV 存储，切换默认上传目标，并对远端做健康检查。</p></div>
        <div class="admin-session">{#if currentUser}<span>当前账号：{currentUser.displayName} / {currentUser.groupName}</span>{:else if authReady}<span>需要管理员登录。</span><a class="inline-link" href="/login">去登录</a>{:else}<span>正在检查登录状态。</span>{/if}</div>
        {#if storageError}<p class="form-error">{storageError}</p>{:else if storageMessage}<p class="form-success">{storageMessage}</p>{/if}
        {#if storageHealthResult}<p class="form-success">{storageHealthResult}</p>{/if}
        <div class="resource-list">
          {#each storageConfigs as config}
            <article class="detail-panel">
              <div class="subsection-heading">
                <h3>{config.name}</h3>
                <p>{config.type === 'local' ? '默认本机文件系统存储。' : config.type === 's3' ? '兼容 MinIO、R2、B2 等 S3 API。' : '通过 WebDAV 协议写入远端存储。'}</p>
              </div>
              <div class="resource-filter-grid">
                <label>名称<input bind:value={config.name} /></label>
                <label>类型<input value={config.type} readonly /></label>
                <label>公共地址<input bind:value={config.publicBaseUrl} placeholder="可选 CDN 或公开访问地址" /></label>
                {#if config.type !== 'local'}
                  <label>端点<input bind:value={config.endpoint} placeholder="https://endpoint.example.com" /></label>
                  <label>基础路径<input bind:value={config.basePath} placeholder="uploads/assets" /></label>
                {/if}
                {#if config.type === 's3'}
                  <label>Region<input bind:value={config.region} placeholder="auto / us-east-1" /></label>
                  <label>Bucket<input bind:value={config.bucket} placeholder="bucket-name" /></label>
                  <label>Access Key<input bind:value={config.accessKeyId} /></label>
                  <label>Secret Key<input bind:value={config.secretAccessKey} type="password" placeholder="留空表示沿用已保存值" /></label>
                  <label class="toggle-row"><span>使用 Path Style</span><input bind:checked={config.usePathStyle} type="checkbox" /></label>
                {:else if config.type === 'webdav'}
                  <label>用户名<input bind:value={config.username} placeholder="可选" /></label>
                  <label>密码<input bind:value={config.password} type="password" placeholder="留空表示沿用已保存值" /></label>
                {/if}
              </div>
              <label class="toggle-row"><span>设为默认上传存储</span><input bind:checked={config.isDefault} type="checkbox" on:change={() => storageConfigs = storageConfigs.map((item) => ({ ...item, isDefault: item.id === config.id }))} /></label>
              <div class="resource-actions">
                <button class="button primary compact" type="button" on:click={() => saveStorageConfig(config)} disabled={savingStorageId === config.id}>{savingStorageId === config.id ? '保存中' : '保存配置'}</button>
                <button class="button secondary compact" type="button" on:click={() => runStorageHealthCheck(config)} disabled={checkingStorageId === config.id}>{checkingStorageId === config.id ? '检查中' : '健康检查'}</button>
              </div>
            </article>
          {/each}
        </div>
      {:else if isSiteSettingsPage}
        <div class="section-heading"><p class="eyebrow">站点配置</p><h2>站点设置</h2><p>配置站点名称、主页模块、游客上传开关和新上传资源的外链域名。</p></div>
        <div class="admin-session">{#if currentUser}<span>当前账号：{currentUser.displayName} / {currentUser.groupName}</span>{:else if authReady}<span>需要管理员登录。</span><a class="inline-link" href="/login">去登录</a>{:else}<span>正在检查登录状态。</span>{/if}</div>
        <article class="detail-panel site-settings-panel">
          <div class="resource-filter-grid">
            <label>站点名称<input bind:value={siteSettingsForm.siteName} /></label>
            <label>外链域名<input bind:value={siteSettingsForm.externalBaseUrl} placeholder="https://cdn.example.com" /></label>
            <label>当前首页标题<input value={siteName} readonly /></label>
          </div>
          <div class="toggle-list">
            <label class="toggle-row"><span>允许游客上传</span><input bind:checked={siteSettingsForm.allowGuestUploads} type="checkbox" /></label>
            <label class="toggle-row"><span>首页显示统计</span><input bind:checked={siteSettingsForm.showStatsOnHome} type="checkbox" /></label>
            <label class="toggle-row"><span>首页显示精选</span><input bind:checked={siteSettingsForm.showFeaturedOnHome} type="checkbox" /></label>
          </div>
          {#if siteSettingsError}<p class="form-error">{siteSettingsError}</p>{:else if siteSettingsMessage}<p class="form-success">{siteSettingsMessage}</p>{/if}
          <div class="resource-actions">
            <button class="button primary compact" type="button" on:click={saveSiteSettings} disabled={isSavingSiteSettings}>{isSavingSiteSettings ? '保存中' : '保存站点设置'}</button>
          </div>
        </article>
      {:else if isCompressionPage}
        <div class="section-heading"><p class="eyebrow">图片压缩</p><h2>动态图片压缩</h2><p>按用户组控制上传图片的压缩质量。系统会使用服务器 CPU 并发处理，只有压缩后更小时才替换原文件。</p></div>
        <div class="admin-session">{#if currentUser}<span>当前账号：{currentUser.displayName} / {currentUser.groupName}</span>{:else if authReady}<span>需要管理员登录。</span><a class="inline-link" href="/login">去登录</a>{:else}<span>正在检查登录状态。</span>{/if}</div>
        {#if userGroupError}<p class="form-error">{userGroupError}</p>{:else if userGroupMessage}<p class="form-success">{userGroupMessage}</p>{/if}
        <div class="resource-list">
          {#each userGroups as group}
            <article class="detail-panel compression-panel">
              <div class="subsection-heading"><h3>{group.name}</h3><p>{group.imageCompressionEnabled ? `当前压缩质量 ${clampCompressionQuality(group.imageCompressionQuality)}%` : '当前关闭图片压缩'}</p></div>
              <div class="compression-control-grid">
                <label class="toggle-row"><span>启用上传图片压缩</span><input bind:checked={group.imageCompressionEnabled} type="checkbox" /></label>
                <label>压缩质量 {clampCompressionQuality(group.imageCompressionQuality)}%<input bind:value={group.imageCompressionQuality} type="range" min="50" max="80" step="1" disabled={!group.imageCompressionEnabled} /></label>
                <label>精确数值<input bind:value={group.imageCompressionQuality} type="number" min="50" max="80" step="1" disabled={!group.imageCompressionEnabled} /></label>
              </div>
              <p class="muted-copy">50% 更小，80% 更接近原图。JPEG 会按质量重编码，PNG 使用无损最高压缩；GIF 保持原文件。</p>
              <div class="resource-actions"><button class="button primary compact" type="button" on:click={() => saveUserGroup(group)} disabled={savingUserGroupId === group.id}>{savingUserGroupId === group.id ? '保存中' : '保存压缩设置'}</button></div>
            </article>
          {/each}
        </div>
      {:else if isFeaturedAdminPage}
        <div class="section-heading"><p class="eyebrow">精选资源</p><h2>精选管理</h2><p>把资源加入探索广场，调整顺序，并控制首页展示的精选内容。</p></div>
        <div class="admin-session">{#if currentUser}<span>当前账号：{currentUser.displayName} / {currentUser.groupName}</span>{:else if authReady}<span>需要管理员登录。</span><a class="inline-link" href="/login">去登录</a>{:else}<span>正在检查登录状态。</span>{/if}</div>
        {#if featuredError}<p class="form-error">{featuredError}</p>{:else if featuredMessage}<p class="form-success">{featuredMessage}</p>{/if}
        <section class="detail-panel">
          <div class="subsection-heading"><h3>当前精选</h3><p>前台探索广场和首页会按此顺序展示资源。</p></div>
          <div class="resource-list">
            {#if featuredResources.length === 0}
              <p>还没有精选资源。</p>
            {:else}
              {#each featuredResources as item, index}
                <article class="resource-row featured-row">
                  <div class="resource-main">
                    <div class="resource-title"><h3>{item.resource.originalName}</h3><span>排序 #{item.sortOrder} · {resourceBadge(item.resource)}</span></div>
                    <a class="inline-link" href={item.resource.publicUrl} target="_blank" rel="noreferrer">{item.resource.publicUrl}</a>
                  </div>
                  <div class="featured-preview">
                    {#if item.resource.type === 'image'}
                      <img src={item.resource.publicUrl} alt={item.resource.originalName} />
                    {:else}
                      <div class="preview-panel muted"><strong>{item.resource.type}</strong><span>{formatBytes(item.resource.size)}</span></div>
                    {/if}
                  </div>
                  <div class="resource-actions">
                    <button class="button ghost compact" type="button" on:click={() => moveFeatured(index, -1)} disabled={index === 0}>上移</button>
                    <button class="button ghost compact" type="button" on:click={() => moveFeatured(index, 1)} disabled={index === featuredResources.length - 1}>下移</button>
                    <button class="button secondary compact" type="button" on:click={() => removeFeatured(item.resource.id)}>下架</button>
                  </div>
                </article>
              {/each}
            {/if}
          </div>
        </section>
        <section class="detail-panel">
          <div class="subsection-heading"><h3>可加入精选的资源</h3><p>从现有资源中挑选要公开展示的内容。</p></div>
          <form class="resource-filter-grid" on:submit|preventDefault={applyResourceFilters}><label>搜索<input bind:value={resourceFilters.search} placeholder="文件名、扩展名或资源 ID" /></label><label>类型<select bind:value={resourceFilters.type}><option value="">全部</option>{#each resourceTypeOptions as option}<option value={option}>{resourceTypeLabel(option)}</option>{/each}</select></label><label>排序<select bind:value={resourceFilters.sort}><option value="created_desc">最新优先</option><option value="created_asc">最早优先</option></select></label><button class="button primary filter-submit" type="submit">应用筛选</button></form>
          <div class="resource-list">
            {#if resources.length === 0}
              <p>当前没有可管理的资源。</p>
            {:else}
              {#each resources as item}
                <article class="resource-row">
                  <div class="resource-main">
                    <div class="resource-title"><h3>{item.originalName}</h3><span>{resourceBadge(item)}</span></div>
                    <a class="inline-link" href={item.publicUrl} target="_blank" rel="noreferrer">{item.publicUrl}</a>
                  </div>
                  <div class="resource-stats-grid">
                    <article><span>创建时间</span><strong>{formatDateTime(item.createdAt)}</strong></article>
                    <article><span>存储驱动</span><strong>{item.storageDriver}</strong></article>
                    <article><span>访问次数</span><strong>{item.accessCount}</strong></article>
                    <article><span>累计流量</span><strong>{formatBytes(item.trafficBytes)}</strong></article>
                  </div>
                  <div class="resource-actions">
                    {#if isFeaturedResource(item.id)}
                      <button class="button ghost compact" type="button" on:click={() => removeFeatured(item.id)}>已精选</button>
                    {:else if item.isPrivate}
                      <button class="button ghost compact" type="button" disabled>私有资源不可精选</button>
                    {:else}
                      <button class="button primary compact" type="button" on:click={() => addFeatured(item)}>加入精选</button>
                    {/if}
                    <a class="button secondary compact" href={`/admin/resources/${item.id}`} on:click|preventDefault={() => navigate(`/admin/resources/${item.id}`)}>查看详情</a>
                  </div>
                </article>
              {/each}
            {/if}
          </div>
          {#if resourceTotalPages > 1}<nav class="pagination" aria-label="精选资源分页"><button class="button ghost compact" type="button" on:click={() => changeResourcePage(resourcePage - 1)} disabled={resourcePage <= 1}>上一页</button>{#each pageRange() as pageNumber}<button class:active-page={pageNumber === resourcePage} class="button ghost compact" type="button" on:click={() => changeResourcePage(pageNumber)}>{pageNumber}</button>{/each}<button class="button ghost compact" type="button" on:click={() => changeResourcePage(resourcePage + 1)} disabled={resourcePage >= resourceTotalPages}>下一页</button></nav>{/if}
        </section>
      {:else if isResourcePage}
        <div class="section-heading"><p class="eyebrow">资源库</p><h2>资源管理</h2><p>支持搜索、筛选、分页、软删除、恢复和详情查看。</p></div>
        <div class="admin-session">{#if currentUser}<span>当前账号：{currentUser.displayName} / {currentUser.groupName}</span>{:else if authReady}<span>需要管理员登录。</span><a class="inline-link" href="/login">去登录</a>{:else}<span>正在检查登录状态。</span>{/if}</div>
        <form class="resource-filter-grid" on:submit|preventDefault={applyResourceFilters}><label>搜索<input bind:value={resourceFilters.search} placeholder="文件名、扩展名或资源 ID" /></label><label>类型<select bind:value={resourceFilters.type}><option value="">全部</option>{#each resourceTypeOptions as option}<option value={option}>{resourceTypeLabel(option)}</option>{/each}</select></label><label>状态<select bind:value={resourceFilters.status}><option value="active">正常</option><option value="deleted">已删除</option><option value="all">全部</option></select></label><label>用户组<select bind:value={resourceFilters.userGroup}><option value="">全部</option>{#each groupOptions as option}<option value={option}>{groupLabel(option)}</option>{/each}</select></label><label>排序<select bind:value={resourceFilters.sort}><option value="created_desc">最新优先</option><option value="created_asc">最早优先</option></select></label><button class="button primary filter-submit" type="submit">应用筛选</button></form>
        {#if resourceError}<p class="form-error">{resourceError}</p>{:else if resourceMessage}<p class="form-success">{resourceMessage}</p>{/if}
        <div class="resource-toolbar"><span>共 {resourceTotal} 条资源，当前每页 {resourcePageSize} 条</span><label class="resource-page-size">每页<select value={resourcePageSize} on:change={(event) => changeResourcePageSize((event.currentTarget as HTMLSelectElement).value)}>{#each resourcePageSizeOptions as option}<option value={option}>{option} 条</option>{/each}</select></label><button class="button secondary compact" type="button" on:click={() => loadResources(resourcePage, true)} disabled={isLoadingResources}>{isLoadingResources ? '刷新中' : '刷新'}</button></div>
        <div class="resource-list capped" aria-live="polite">{#if isLoadingResources}<p>加载资源中…</p>{:else if resources.length === 0}<p>当前筛选条件下没有资源。</p>{:else}{#each resources as item}<article class="resource-row"><div class="resource-main"><div class="resource-title"><h3>{item.originalName}</h3><span>{resourceBadge(item)}</span></div><a class="inline-link" href={item.publicUrl} target="_blank" rel="noreferrer">{item.publicUrl}</a><p>创建于 {formatDateTime(item.createdAt)}</p></div><dl class="resource-stats-grid"><div><dt>访问</dt><dd>{item.accessCount}</dd></div><div><dt>总流量</dt><dd>{formatBytes(item.trafficBytes)}</dd></div><div><dt>月流量</dt><dd>{formatBytes(item.monthlyTraffic)} / {formatBytes(item.monthlyLimit)}</dd></div></dl><div class="resource-actions"><a class="button ghost compact" href={`/admin/resources/${item.id}`} on:click|preventDefault={() => navigate(`/admin/resources/${item.id}`)}>详情</a>{#if currentUser}{#if item.status === 'deleted'}<button class="button secondary compact" type="button" on:click={() => restoreResource(item.id)}>恢复</button>{:else}<button class="button secondary compact" type="button" on:click={() => deleteResource(item.id)}>删除</button>{/if}{/if}</div></article>{/each}{/if}</div>
        {#if resourceTotalPages > 1}<nav class="pagination" aria-label="资源分页"><button class="button ghost compact" type="button" on:click={() => changeResourcePage(resourcePage - 1)} disabled={resourcePage <= 1}>上一页</button>{#each pageRange() as pageNumber}<button class:active-page={pageNumber === resourcePage} class="button ghost compact" type="button" on:click={() => changeResourcePage(pageNumber)}>{pageNumber}</button>{/each}<button class="button ghost compact" type="button" on:click={() => changeResourcePage(resourcePage + 1)} disabled={resourcePage >= resourceTotalPages}>下一页</button></nav>{/if}
      {/if}
    </section>
  </main>
  {/if}
{:else if isExplorePage}
  <main class="page-shell wide">
    <a class="back-link" href="/" on:click|preventDefault={() => navigate('/')}>返回首页</a>
    <section class="glass-panel page-panel">
      <div class="panel-head">
        <div>
          <p class="eyebrow">探索广场</p>
          <h1>探索广场</h1>
          <p class="lead compact">精选资源以画廊形式展示，每页最多 {exploreFeaturedPageSize} 个，点击卡片可查看访问、流量和存储信息。</p>
        </div>
        <div class="summary-card"><span>精选数量</span><strong>{featuredResources.length}</strong><small>第 {explorePage} / {exploreTotalPages()} 页</small></div>
      </div>
      {#if featuredError}<p class="form-error">{featuredError}</p>{/if}
      <div class="gallery-grid explore-gallery" aria-live="polite">
        {#if !featuredReady}
          <p>精选资源加载中…</p>
        {:else if featuredResources.length === 0}
          <p>当前还没有公开精选资源。</p>
        {:else}
          {#each exploreFeaturedResources() as item}
            <article class="gallery-card">
              {#if item.resource.type === 'image'}
                <div class="gallery-preview">
                  <img loading="lazy" src={item.resource.publicUrl} alt={item.resource.originalName} />
                </div>
              {:else}
                <div class="gallery-preview muted">
                  <strong>{resourceTypeLabel(item.resource.type)}</strong>
                  <span>{formatBytes(item.resource.size)}</span>
                </div>
              {/if}
              <div class="gallery-card-body">
                <p class="gallery-kind">{resourceTypeLabel(item.resource.type)} · {formatBytes(item.resource.size)}</p>
                <h3>{item.resource.originalName}</h3>
                <dl>
                  <div><dt>访问</dt><dd>{item.resource.accessCount}</dd></div>
                  <div><dt>流量</dt><dd>{formatBytes(item.resource.trafficBytes)}</dd></div>
                </dl>
              </div>
              <div class="gallery-actions">
                <button class="button primary compact" type="button" on:click={() => openGalleryModal(item.resource)}>查看数据</button>
                <a class="button secondary compact" href={item.resource.publicUrl} target="_blank" rel="noreferrer">打开资源</a>
              </div>
            </article>
          {/each}
        {/if}
      </div>
      {#if featuredReady && exploreTotalPages() > 1}
        <nav class="pagination gallery-pagination" aria-label="探索广场分页">
          <button class="button ghost compact" type="button" on:click={() => changeExplorePage(explorePage - 1)} disabled={explorePage <= 1}>上一页</button>
          {#each explorePageRange() as pageNumber}
            <button class:active-page={pageNumber === explorePage} class="button ghost compact" type="button" on:click={() => changeExplorePage(pageNumber)} aria-current={pageNumber === explorePage ? 'page' : undefined}>{pageNumber}</button>
          {/each}
          <button class="button ghost compact" type="button" on:click={() => changeExplorePage(explorePage + 1)} disabled={explorePage >= exploreTotalPages()}>下一页</button>
        </nav>
      {/if}
    </section>
  </main>
{:else}
  <main class="home-shell">
    <section class:single-panel={!siteSettings.showStatsOnHome} class="home-stage">
      <div class="home-copy">
        <p class="eyebrow">马赫环静态托管</p>
        <h1>{siteName}</h1>
        <p class="lead">统一托管图片、脚本、压缩包、可执行文件与其他静态资源，大陆优化网络，免登录上传。</p>
        <div class="actions" aria-label="主要操作">{#if installState?.initialized}<a class="button primary" href="/upload">上传</a><a class="button secondary" href="/explore">探索广场</a>{#if currentUser}<a class="button ghost" href="/account">账户</a>{:else}<a class="button ghost" href="/login">登录</a>{/if}{#if !currentUser || currentUser.role === 'admin'}<a class="button secondary" href="/admin" on:click|preventDefault={() => navigate('/admin')}>后台</a>{/if}{:else if isKnownUninitialized()}<a class="button primary" href="/install">初始化</a>{:else}<span class="button ghost disabled">状态检查失败</span>{/if}</div>
      </div>
      {#if siteSettings.showStatsOnHome}
        <div class="hero-stats">
          <div class="hero-grid">
            <article class="metric-card glass-panel"><span>资源总数</span><strong>{homeStats.totalResources}</strong><small>正常 {homeStats.activeResources} 条</small></article>
            <article class="metric-card glass-panel"><span>累计存储</span><strong>{formatBytes(homeStats.totalStorageBytes)}</strong><small>当前有效资源占用</small></article>
            <article class="metric-card glass-panel"><span>累计流量</span><strong>{formatBytes(homeStats.totalTrafficBytes)}</strong><small>所有资源历史访问总量</small></article>
            <article class="metric-card glass-panel"><span>今日上传</span><strong>{homeStats.todayUploads}</strong><small>实时刷新</small></article>
          </div>
        </div>
      {/if}
    </section>
    {#if siteSettings.showFeaturedOnHome}
      <section class="glass-panel page-panel home-featured">
        <div class="panel-head">
          <div>
            <p class="eyebrow">Featured</p>
            <h2>精选资源</h2>
            <p class="lead compact">首页最多展示 {homeFeaturedLimit} 个精选资源，更多内容进入探索广场翻页浏览。</p>
          </div>
          <a class="button secondary compact" href="/explore">查看全部{#if featuredOverflowCount() > 0} +{featuredOverflowCount()}{/if}</a>
        </div>
        <div class="gallery-grid home-gallery">
          {#if featuredResources.length === 0}
            <p>当前还没有精选资源。</p>
          {:else}
            {#each homeFeaturedResources() as item}
              <article class="gallery-card compact-gallery-card">
                {#if item.resource.type === 'image'}
                  <div class="gallery-preview">
                    <img loading="lazy" src={item.resource.publicUrl} alt={item.resource.originalName} />
                  </div>
                {:else}
                  <div class="gallery-preview muted"><strong>{resourceTypeLabel(item.resource.type)}</strong><span>{formatBytes(item.resource.size)}</span></div>
                {/if}
                <div class="gallery-card-body">
                  <p class="gallery-kind">{resourceTypeLabel(item.resource.type)} · {formatBytes(item.resource.size)}</p>
                  <h3>{item.resource.originalName}</h3>
                </div>
                <div class="gallery-actions">
                  <button class="button primary compact" type="button" on:click={() => openGalleryModal(item.resource)}>查看数据</button>
                </div>
              </article>
            {/each}
          {/if}
        </div>
      </section>
    {/if}
  </main>
{/if}

{#if galleryModalResource}
  <div class="gallery-modal-backdrop" role="presentation" on:click|self={closeGalleryModal}>
    <div class="gallery-modal" role="dialog" aria-modal="true" aria-labelledby="gallery-modal-title">
      <button class="modal-close" type="button" aria-label="关闭资源数据弹窗" on:click={closeGalleryModal}>×</button>
      <div class="gallery-modal-preview">
        {#if galleryModalResource.type === 'image'}
          <img src={galleryModalResource.publicUrl} alt={galleryModalResource.originalName} />
        {:else}
          <div class="gallery-preview muted"><strong>{resourceTypeLabel(galleryModalResource.type)}</strong><span>{formatBytes(galleryModalResource.size)}</span></div>
        {/if}
      </div>
      <div class="gallery-modal-info">
        <p class="eyebrow">Resource Data</p>
        <h2 id="gallery-modal-title">{galleryModalResource.originalName}</h2>
        <p class="lead compact">{securityHint(galleryModalResource)}</p>
        <dl class="gallery-data-grid">
          <div><dt>类型</dt><dd>{resourceTypeLabel(galleryModalResource.type)}</dd></div>
          <div><dt>大小</dt><dd>{formatBytes(galleryModalResource.size)}</dd></div>
          <div><dt>访问次数</dt><dd>{galleryModalResource.accessCount}</dd></div>
          <div><dt>累计流量</dt><dd>{formatBytes(galleryModalResource.trafficBytes)}</dd></div>
          <div><dt>本月流量</dt><dd>{monthlyUsageLabel(galleryModalResource)}</dd></div>
          <div><dt>上传时间</dt><dd>{formatDateTime(galleryModalResource.createdAt)}</dd></div>
          <div><dt>存储驱动</dt><dd>{galleryModalResource.storageDriver}</dd></div>
          <div><dt>可见性</dt><dd>{galleryModalResource.isPrivate ? '私有' : '公开'}</dd></div>
        </dl>
        <div class="gallery-link-box">
          <span>{galleryModalResource.publicUrl}</span>
          <button class="button secondary compact" type="button" on:click={() => copyToClipboard(galleryModalResource?.publicUrl ?? '')}>复制直链</button>
        </div>
        {#if copyMessage}<p class="form-success">{copyMessage}</p>{/if}
        <div class="resource-actions">
          <a class="button primary compact" href={galleryModalResource.publicUrl} target="_blank" rel="noreferrer">打开资源</a>
          <button class="button ghost compact" type="button" on:click={closeGalleryModal}>关闭</button>
        </div>
      </div>
    </div>
  </div>
{/if}
