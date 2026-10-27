/* RFE Tonal - part 5: localization (EN/DE/ES/AR with RTL), accessibility, offline states for new pages, About and support. */
(function () {
  'use strict';
  const A = window.A, E = A.E, U = A.U, S = A.S, $ = A.$, $$ = A.$$, esc = A.esc, ic = A.ic, st = A.state, X = A.X, H = A.fxHandlers;
  const THUMB = /[?&](thumb|selftest)=1/.test(location.search);
    const root = document.documentElement;
  const q0 = new URLSearchParams(location.search);
  ['lang', 'zoom', 'contrast'].forEach((k) => { if (q0.get(k)) S[k === 'contrast' ? 'highContrast' : k === 'zoom' ? 'textSize' : 'lang'] = k === 'contrast' ? q0.get(k) === 'high' : k === 'zoom' ? +q0.get(k) : q0.get(k); });
  if (S.lang === undefined) S.lang = 'en'; if (S.textSize === undefined) S.textSize = 100; if (S.reduceMotion === undefined) S.reduceMotion = false; if (S.highContrast === undefined) S.highContrast = !!(window.matchMedia && matchMedia('(prefers-contrast: more)').matches); /* follows the system until the owner chooses */

  /* ================= localization ================= */
  const LANGS = [['en', 'English'], ['de', 'Deutsch'], ['es', 'Español'], ['ar', 'العربية']];
  /* [en, de, es, ar] */
  const D = [
    ['folder', 'Ordner', 'carpeta', 'مجلد'], ['file', 'Datei', 'archivo', 'ملف'],
    ['Loading…', 'Wird geladen…', 'Cargando…', 'جارٍ التحميل…'], ['Too large to preview', 'Zu groß für die Vorschau', 'Demasiado grande para la vista previa', 'كبير جدًا للمعاينة'], ['No preview', 'Keine Vorschau', 'Sin vista previa', 'لا توجد معاينة'],
    ['Fit', 'Einpassen', 'Ajustar', 'ملاءمة'], ['Zoom in', 'Vergrößern', 'Acercar', 'تكبير'], ['Zoom out', 'Verkleinern', 'Alejar', 'تصغير'], ['Previous file', 'Vorherige Datei', 'Archivo anterior', 'الملف السابق'], ['Next file', 'Nächste Datei', 'Archivo siguiente', 'الملف التالي'],
    ['Download to read it', 'Zum Lesen herunterladen', 'Descargar para leerlo', 'نزّل للقراءة'], ['Download to open it', 'Zum Öffnen herunterladen', 'Descargar para abrirlo', 'نزّل للفتح'],
    ['Files', 'Dateien', 'Archivos', 'الملفات'], ['Servers', 'Server', 'Servidores', 'الخوادم'], ['Devices', 'Geräte', 'Dispositivos', 'الأجهزة'], ['Transfers', 'Übertragungen', 'Transferencias', 'عمليات النقل'], ['Search', 'Suche', 'Buscar', 'بحث'], ['Tools', 'Werkzeuge', 'Herramientas', 'الأدوات'], ['History', 'Verlauf', 'Historial', 'السجل'], ['Settings', 'Einstellungen', 'Ajustes', 'الإعدادات'],
    ['All', 'Alle', 'Todo', 'الكل'], ['Folders', 'Ordner', 'Carpetas', 'المجلدات'], ['Packages', 'Pakete', 'Paquetes', 'الحزم'], ['Media', 'Medien', 'Multimedia', 'الوسائط'], ['Archives', 'Archive', 'Archivos comprimidos', 'الأرشيفات'], ['Over 100 MB', 'Über 100 MB', 'Más de 100 MB', 'أكثر من 100 ميغابايت'], ['Filter this folder', 'Ordner filtern', 'Filtrar esta carpeta', 'تصفية هذا المجلد'],
    ['Name', 'Name', 'Nombre', 'الاسم'], ['Size', 'Größe', 'Tamaño', 'الحجم'], ['Modified', 'Geändert', 'Modificado', 'آخر تعديل'], ['Permissions', 'Berechtigungen', 'Permisos', 'الأذونات'], ['List', 'Liste', 'Lista', 'قائمة'],
    ['New folder', 'Neuer Ordner', 'Nueva carpeta', 'مجلد جديد'], ['Upload files', 'Dateien hochladen', 'Subir archivos', 'رفع ملفات'], ['Upload', 'Hochladen', 'Subir', 'رفع'], ['Download', 'Herunterladen', 'Descargar', 'تنزيل'], ['Preview', 'Vorschau', 'Vista previa', 'معاينة'], ['Rename', 'Umbenennen', 'Renombrar', 'إعادة تسمية'], ['Delete', 'Löschen', 'Eliminar', 'حذف'], ['Details', 'Details', 'Detalles', 'التفاصيل'], ['Local files', 'Lokale Dateien', 'Archivos locales', 'الملفات المحلية'],
    ['Copy to…', 'Kopieren nach…', 'Copiar a…', 'نسخ إلى…'], ['Move to…', 'Verschieben nach…', 'Mover a…', 'نقل إلى…'], ['Compress', 'Komprimieren', 'Comprimir', 'ضغط'], ['Rename…', 'Umbenennen…', 'Renombrar…', 'إعادة تسمية…'], ['Edit', 'Bearbeiten', 'Editar', 'تحرير'], ['Save as…', 'Speichern unter…', 'Guardar como…', 'حفظ باسم…'], ['Extract here', 'Hier entpacken', 'Extraer aquí', 'استخراج هنا'],
    ['Active', 'Aktiv', 'Activas', 'نشطة'], ['Needs attention', 'Braucht Aufmerksamkeit', 'Requiere atención', 'تحتاج إلى انتباه'], ['Queued', 'In Warteschlange', 'En cola', 'في الانتظار'], ['Done', 'Fertig', 'Completadas', 'مكتملة'], ['Pause all', 'Alle pausieren', 'Pausar todo', 'إيقاف الكل مؤقتًا'], ['Clear done', 'Erledigte entfernen', 'Quitar completadas', 'مسح المكتمل'],
    ['Cancel', 'Abbrechen', 'Cancelar', 'إلغاء'], ['Close', 'Schließen', 'Cerrar', 'إغلاق'], ['Save', 'Speichern', 'Guardar', 'حفظ'], ['Discard', 'Verwerfen', 'Descartar', 'تجاهل'], ['Keep editing', 'Weiter bearbeiten', 'Seguir editando', 'متابعة التحرير'], ['Open', 'Öffnen', 'Abrir', 'فتح'], ['Dismiss', 'Ausblenden', 'Descartar', 'إخفاء'], ['Retry', 'Erneut versuchen', 'Reintentar', 'إعادة المحاولة'], ['Undo', 'Rückgängig', 'Deshacer', 'تراجع'], ['Restore', 'Wiederherstellen', 'Restaurar', 'استعادة'],
    ['Approve', 'Genehmigen', 'Aprobar', 'موافقة'], ['Deny', 'Ablehnen', 'Denegar', 'رفض'], ['Revoke', 'Widerrufen', 'Revocar', 'إلغاء الصلاحية'], ['Next', 'Weiter', 'Siguiente', 'التالي'], ['Back', 'Zurück', 'Atrás', 'رجوع'], ['Get started', 'Los geht’s', 'Comenzar', 'ابدأ'], ['Skip', 'Überspringen', 'Omitir', 'تخطي'], ['Show', 'Anzeigen', 'Mostrar', 'إظهار'], ['View', 'Ansehen', 'Ver', 'عرض'],
    ['Settings', 'Einstellungen', 'Ajustes', 'الإعدادات'], ['Changes apply immediately and are kept in this browser.', 'Änderungen gelten sofort und werden in diesem Browser gespeichert.', 'Los cambios se aplican al instante y se guardan en este navegador.', 'تُطبَّق التغييرات فورًا وتُحفظ في هذا المتصفح.'],
    ['Connection', 'Verbindung', 'Conexión', 'الاتصال'], ['Appearance', 'Darstellung', 'Apariencia', 'المظهر'], ['Security', 'Sicherheit', 'Seguridad', 'الأمان'], ['Desktop', 'Desktop', 'Escritorio', 'سطح المكتب'], ['Backup', 'Sicherung', 'Copia de seguridad', 'النسخ الاحتياطي'], ['About', 'Über', 'Acerca de', 'حول'], 
    ['Parallel transfers', 'Parallele Übertragungen', 'Transferencias en paralelo', 'عمليات النقل المتوازية'], ['How many files move at once', 'Wie viele Dateien gleichzeitig übertragen werden', 'Cuántos archivos se mueven a la vez', 'عدد الملفات المنقولة في وقت واحد'], ['Speed limit', 'Geschwindigkeitslimit', 'Límite de velocidad', 'حد السرعة'], ['None', 'Keins', 'Ninguno', 'بلا حد'],
    ['Verify checksums', 'Prüfsummen prüfen', 'Verificar sumas de comprobación', 'التحقق من المجاميع الاختبارية'], ['Download folder', 'Download-Ordner', 'Carpeta de descargas', 'مجلد التنزيل'], ['Where downloads land', 'Wohin Downloads gespeichert werden', 'Dónde se guardan las descargas', 'مكان حفظ التنزيلات'],
    ['Reconnect automatically', 'Automatisch neu verbinden', 'Reconectar automáticamente', 'إعادة الاتصال تلقائيًا'], ['Notify when a transfer finishes', 'Benachrichtigen, wenn eine Übertragung endet', 'Avisar al terminar una transferencia', 'التنبيه عند انتهاء النقل'], ['Notify about errors', 'Bei Fehlern benachrichtigen', 'Avisar de errores', 'التنبيه عند حدوث أخطاء'],
    ['Theme', 'Design', 'Tema', 'السمة'], ['Light', 'Hell', 'Claro', 'فاتح'], ['Dark', 'Dunkel', 'Oscuro', 'داكن'], ['System', 'System', 'Sistema', 'النظام'], ['Row density', 'Zeilendichte', 'Densidad de filas', 'كثافة الصفوف'], ['Comfortable', 'Komfortabel', 'Cómoda', 'مريحة'], ['Compact', 'Kompakt', 'Compacta', 'مدمجة'],
    ['Language', 'Sprache', 'Idioma', 'اللغة'], ['Text size', 'Textgröße', 'Tamaño del texto', 'حجم النص'], ['Reduce motion', 'Bewegung reduzieren', 'Reducir movimiento', 'تقليل الحركة'], ['High contrast', 'Hoher Kontrast', 'Alto contraste', 'تباين عالٍ'], ['Language and accessibility', 'Sprache und Barrierefreiheit', 'Idioma y accesibilidad', 'اللغة وإمكانية الوصول'],
    ['Approve new devices here', 'Neue Geräte hier genehmigen', 'Aprobar dispositivos nuevos aquí', 'الموافقة على الأجهزة الجديدة هنا'], ['Sign out all phones', 'Alle Telefone abmelden', 'Cerrar sesión en todos los teléfonos', 'تسجيل خروج جميع الهواتف'], ['Sign out', 'Abmelden', 'Cerrar sesión', 'تسجيل الخروج'],
    ['Close to the system tray', 'In den Infobereich schließen', 'Cerrar a la bandeja del sistema', 'الإغلاق إلى منطقة الإشعارات'], ['Start RFE when I sign in', 'RFE beim Anmelden starten', 'Iniciar RFE al iniciar sesión', 'تشغيل RFE عند تسجيل الدخول'], ['Desktop notifications', 'Desktop-Benachrichtigungen', 'Notificaciones de escritorio', 'إشعارات سطح المكتب'], ['Test notification', 'Testbenachrichtigung', 'Notificación de prueba', 'إشعار تجريبي'], ['Tray menu', 'Tray-Menü', 'Menú de la bandeja', 'قائمة منطقة الإشعارات'],
    ['Back up settings', 'Einstellungen sichern', 'Copia de seguridad de ajustes', 'نسخ الإعدادات احتياطيًا'], ['Restore settings', 'Einstellungen wiederherstellen', 'Restaurar ajustes', 'استعادة الإعدادات'], ['Show hidden files', 'Versteckte Dateien anzeigen', 'Mostrar archivos ocultos', 'إظهار الملفات المخفية'], ['Ask where to save downloads', 'Beim Download nach Speicherort fragen', 'Preguntar dónde guardar las descargas', 'السؤال عن مكان حفظ التنزيلات'],
    ['Move deleted items to Trash', 'Gelöschtes in den Papierkorb verschieben', 'Mover lo eliminado a la papelera', 'نقل المحذوفات إلى سلة المهملات'], ['Check for updates', 'Nach Updates suchen', 'Buscar actualizaciones', 'التحقق من وجود تحديثات'], ['Update available', 'Update verfügbar', 'Actualización disponible', 'يتوفر تحديث'], ['Download and install', 'Herunterladen und installieren', 'Descargar e instalar', 'تنزيل وتثبيت'], ['Restart now', 'Jetzt neu starten', 'Reiniciar ahora', 'إعادة التشغيل الآن'],
    ['Pair a phone', 'Telefon koppeln', 'Vincular un teléfono', 'إقران هاتف'], ['Paired devices', 'Gekoppelte Geräte', 'Dispositivos vinculados', 'الأجهزة المقترنة'], ['This computer', 'Dieser Computer', 'Este equipo', 'هذا الكمبيوتر'], ['Controller', 'Steuerung', 'Controlador', 'المتحكم'], ['Recent security events', 'Letzte Sicherheitsereignisse', 'Eventos de seguridad recientes', 'أحدث أحداث الأمان'], ['Online now', 'Jetzt online', 'En línea ahora', 'متصل الآن'],
    ['Favorites & offline', 'Favoriten & offline', 'Favoritos y sin conexión', 'المفضلة وغير المتصل'], ['Storage', 'Speicher', 'Almacenamiento', 'التخزين'], ['Trash', 'Papierkorb', 'Papelera', 'سلة المهملات'], ['Share links', 'Freigabelinks', 'Enlaces compartidos', 'روابط المشاركة'], ['Backup & sync', 'Sicherung & Sync', 'Copia y sincronización', 'النسخ والمزامنة'], ['Apps', 'Apps', 'Aplicaciones', 'التطبيقات'], ['Inbox', 'Eingang', 'Bandeja de entrada', 'الوارد'],
    ['Connections', 'Verbindungen', 'Conexiones', 'الاتصالات'], ['Errors', 'Fehler', 'Errores', 'الأخطاء'], ['Clear history', 'Verlauf löschen', 'Borrar historial', 'مسح السجل'], ['Export CSV', 'CSV exportieren', 'Exportar CSV', 'تصدير CSV'], ['No transfers yet', 'Noch keine Übertragungen', 'Aún no hay transferencias', 'لا توجد عمليات نقل بعد'],
    ['Add computer', 'Computer hinzufügen', 'Añadir equipo', 'إضافة كمبيوتر'], ['Welcome', 'Willkommen', 'Bienvenido', 'مرحبًا'], ['Wrap lines', 'Zeilen umbrechen', 'Ajustar líneas', 'التفاف الأسطر'], ['Unsaved changes', 'Ungespeicherte Änderungen', 'Cambios sin guardar', 'تغييرات غير محفوظة'], ['Read only', 'Schreibgeschützt', 'Solo lectura', 'للقراءة فقط'], ['Saved', 'Gespeichert', 'Guardado', 'تم الحفظ'],
    ['Saved searches', 'Gespeicherte Suchen', 'Búsquedas guardadas', 'عمليات البحث المحفوظة'], ['Recent', 'Zuletzt', 'Recientes', 'الأخيرة'], ['Search a server', 'Server durchsuchen', 'Buscar en un servidor', 'البحث في خادم'], ['Delete permanently', 'Endgültig löschen', 'Eliminar definitivamente', 'حذف نهائي'], ['Empty Trash', 'Papierkorb leeren', 'Vaciar papelera', 'إفراغ سلة المهملات'],
    ['Resume all', 'Alle fortsetzen', 'Reanudar todo', 'استئناف الكل'], ['Resume unfinished transfers?', 'Unfertige Übertragungen fortsetzen?', '¿Reanudar las transferencias sin terminar?', 'هل تريد استئناف عمليات النقل غير المكتملة؟'], ['Transfer journal', 'Übertragungsprotokoll', 'Diario de transferencias', 'سجل النقل'],
    ['About RFE', 'Über RFE', 'Acerca de RFE', 'حول RFE'], ['Welcome tour', 'Willkommenstour', 'Recorrido de bienvenida', 'جولة الترحيب'], ['Show again', 'Erneut zeigen', 'Mostrar de nuevo', 'عرض مرة أخرى'], ['Report a problem', 'Problem melden', 'Informar de un problema', 'الإبلاغ عن مشكلة'], ['Copy diagnostics', 'Diagnose kopieren', 'Copiar diagnóstico', 'نسخ التشخيص'], 
    ['Pairing code', 'Kopplungscode', 'Código de vinculación', 'رمز الاقتران'], ['Account', 'Konto', 'Cuenta', 'الحساب'], ['Ask the PC', 'Den PC fragen', 'Preguntar al PC', 'اسأل الكمبيوتر'], ['Diagnose', 'Diagnose', 'Diagnosticar', 'تشخيص'], ['Show hidden files', 'Versteckte Dateien anzeigen', 'Mostrar archivos ocultos', 'إظهار الملفات المخفية'],
    ['Skip to content', 'Zum Inhalt springen', 'Saltar al contenido', 'انتقل إلى المحتوى'], ['No computer is online', 'Kein Computer ist online', 'Ningún equipo está en línea', 'لا يوجد كمبيوتر متصل'], ['Go to Servers', 'Zu den Servern', 'Ir a Servidores', 'الذهاب إلى الخوادم'], 
    ['Shared across running transfers', 'Gilt für alle laufenden Übertragungen', 'Compartido entre las transferencias en curso', 'مشترك بين عمليات النقل الجارية'], ['When a name already exists', 'Wenn der Name schon existiert', 'Cuando el nombre ya existe', 'عند وجود الاسم مسبقًا'], ['Applies to new transfers', 'Gilt für neue Übertragungen', 'Se aplica a las transferencias nuevas', 'يسري على عمليات النقل الجديدة'], ['Ask', 'Fragen', 'Preguntar', 'اسأل'], ['Replace', 'Ersetzen', 'Reemplazar', 'استبدال'], ['Keep both', 'Beide behalten', 'Conservar ambos', 'الاحتفاظ بالاثنين'],
    ['Compare SHA-256 after each file', 'SHA-256 nach jeder Datei vergleichen', 'Comparar SHA-256 tras cada archivo', 'مقارنة SHA-256 بعد كل ملف'], ['Retries 3 times, 8 seconds apart, then resumes transfers', 'Versucht es 3-mal im Abstand von 8 Sekunden und setzt dann Übertragungen fort', 'Reintenta 3 veces, cada 8 segundos, y luego reanuda las transferencias', 'يعيد المحاولة 3 مرات بفاصل 8 ثوانٍ ثم يستأنف النقل'], 
    ['Compact fits more rows', 'Kompakt zeigt mehr Zeilen', 'Compacta muestra más filas', 'المدمج يعرض صفوفًا أكثر'],
    ['Menus and messages. Names and paths are never translated.', 'Menüs und Meldungen. Namen und Pfade werden nie übersetzt.', 'Menús y mensajes. Los nombres y rutas nunca se traducen.', 'القوائم والرسائل. لا تُترجم الأسماء والمسارات أبدًا.'], ['Scales the whole app', 'Skaliert die ganze App', 'Escala toda la aplicación', 'يغيّر حجم التطبيق كله'], ['Turns off animations and transitions', 'Schaltet Animationen und Übergänge aus', 'Desactiva animaciones y transiciones', 'يوقف الرسوم المتحركة والانتقالات'], ['Stronger borders and text colors', 'Stärkere Rahmen und Textfarben', 'Bordes y colores de texto más marcados', 'حدود وألوان نص أوضح'],
    ['A scanned code alone never grants access', 'Ein gescannter Code allein gewährt nie Zugriff', 'Un código escaneado nunca concede acceso por sí solo', 'الرمز الممسوح وحده لا يمنح الوصول'], ['Pairing code lifetime', 'Gültigkeit des Kopplungscodes', 'Duración del código de vinculación', 'مدة صلاحية رمز الاقتران'], ['Codes also work only once', 'Codes funktionieren nur einmal', 'Los códigos solo funcionan una vez', 'الرموز تعمل مرة واحدة فقط'], ['Pinned certificates', 'Angeheftete Zertifikate', 'Certificados fijados', 'الشهادات المثبّتة'],
    ['Closing the window keeps transfers running in the tray', 'Beim Schließen laufen Übertragungen im Infobereich weiter', 'Al cerrar la ventana las transferencias siguen en la bandeja', 'عند إغلاق النافذة تستمر عمليات النقل في منطقة الإشعارات'], ['Starts minimized to the tray', 'Startet minimiert im Infobereich', 'Se inicia minimizado en la bandeja', 'يبدأ مصغّرًا في منطقة الإشعارات'], ['Shown by your operating system', 'Vom Betriebssystem angezeigt', 'Las muestra tu sistema operativo', 'يعرضها نظام التشغيل'], ['New device requests', 'Anfragen neuer Geräte', 'Solicitudes de dispositivos nuevos', 'طلبات الأجهزة الجديدة'],
    ['A phone is waiting for approval', 'Ein Telefon wartet auf Genehmigung', 'Un teléfono espera aprobación', 'هاتف ينتظر الموافقة'], ['Files shared from a phone', 'Vom Telefon geteilte Dateien', 'Archivos compartidos desde un teléfono', 'ملفات مشاركة من هاتف'], ['Updates', 'Updates', 'Actualizaciones', 'التحديثات'], ['A new version is available', 'Eine neue Version ist verfügbar', 'Hay una versión nueva disponible', 'يتوفر إصدار جديد'], ['Try it', 'Ausprobieren', 'Probar', 'جرّبها'], ['Shows how a notification and the tray menu look', 'Zeigt, wie Benachrichtigung und Tray-Menü aussehen', 'Muestra cómo se ven la notificación y el menú de la bandeja', 'يعرض شكل الإشعار وقائمة منطقة الإشعارات'],
    ['Select a file to preview it. Drag files here from the Local files tab to upload.', 'Wähle eine Datei für die Vorschau. Zum Hochladen Dateien aus „Lokale Dateien“ hierher ziehen.', 'Elige un archivo para verlo. Arrastra aquí archivos de la pestaña Archivos locales para subirlos.', 'اختر ملفًا لمعاينته. اسحب الملفات هنا من تبويب الملفات المحلية لرفعها.'], ['Location', 'Ort', 'Ubicación', 'الموقع'], ['Server', 'Server', 'Servidor', 'الخادم'], ['Connected', 'Verbunden', 'Conectado', 'متصل'], ['Offline', 'Offline', 'Sin conexión', 'غير متصل'], ['Connecting…', 'Verbinde…', 'Conectando…', 'جارٍ الاتصال…'],
    ['Nothing here yet', 'Noch nichts vorhanden', 'Aún no hay nada', 'لا شيء هنا بعد'], ['Select all', 'Alle auswählen', 'Seleccionar todo', 'تحديد الكل'], ['Pause', 'Pausieren', 'Pausar', 'إيقاف مؤقت'], ['Resume', 'Fortsetzen', 'Reanudar', 'استئناف'], ['Remove', 'Entfernen', 'Quitar', 'إزالة'], ['Copy', 'Kopieren', 'Copiar', 'نسخ'], ['Copied', 'Kopiert', 'Copiado', 'تم النسخ'], ['Confirm', 'Bestätigen', 'Confirmar', 'تأكيد']
  ];
  const IDX = { de: 1, es: 2, ar: 3 }; const MAP = { de: {}, es: {}, ar: {} };
  D.forEach((r) => { Object.keys(IDX).forEach((l) => { MAP[l][r[0]] = r[IDX[l]]; }); });
  /* The number in a matched count: Western or Arabic-Indic digits, with or without grouping. */
  const cnt = (n) => +String(n).replace(/[\u0660-\u0669]/g, (d) => d.charCodeAt(0) - 0x660).replace(/[^\d]/g, '');
  const RX = {
    de: [[/^([\d.,\u00a0\u202f\u066c\u0660-\u0669]+) items?$/, (m, n) => n + (cnt(n) === 1 ? ' Element' : ' Elemente')], [/^(\d+) selected$/, '$1 ausgewählt'], [/^Search in (.+)$/, 'In $1 suchen'], [/^(\d+) selected · (.+)$/, '$1 ausgewählt · $2'], [/^(.+) free$/, '$1 frei']],
    es: [[/^([\d.,\u00a0\u202f\u066c\u0660-\u0669]+) items?$/, (m, n) => n + (cnt(n) === 1 ? ' elemento' : ' elementos')], [/^(\d+) selected$/, '$1 seleccionados'], [/^Search in (.+)$/, 'Buscar en $1'], [/^(\d+) selected · (.+)$/, '$1 seleccionados · $2'], [/^(.+) free$/, '$1 libres']],
    ar: [[/^([\d.,\u00a0\u202f\u066c\u0660-\u0669]+) items?$/, (m, n) => { const f = new Intl.PluralRules('ar').select(cnt(n)); return f === 'zero' ? 'لا عناصر' : f === 'one' ? 'عنصر واحد' : f === 'two' ? 'عنصران' : f === 'few' ? n + ' عناصر' : f === 'many' ? n + ' عنصرًا' : n + ' عنصر'; }], [/^(\d+) selected$/, '$1 محدد'], [/^Search in (.+)$/, 'ابحث في $1'], [/^(\d+) selected · (.+)$/, '$1 محدد · $2'], [/^(.+) free$/, '$1 متاح']]
  };
  const tr = (s, lang) => { const t = s.trim(); if (!t) return s; let r = MAP[lang][t]; if (r === undefined) { for (const [re, to] of RX[lang]) if (re.test(t)) { r = t.replace(re, to); break; } } if (r === undefined && /^[\d.,\u00a0\u202f\u066c\u0660-\u0669]+ items? · /.test(t)) r = t.split(' · ').map((p) => tr(p, lang).trim()).join(' · '); return r === undefined ? s : s.replace(t, r); };
  const SKIP = 'script,style,textarea,input,select,.mono,code,pre,[data-nolocal],.crumb,.codev,.csvv,.fp,.hrow time';
  const recs = new WeakMap(); const arecs = new WeakMap(); let busy = false;
  function doText(n) {
    const p = n.parentElement; if (!p || p.closest(SKIP)) return; const rec = recs.get(n); const en = rec && n.nodeValue === rec.out ? rec.en : n.nodeValue; const out = S.lang === 'en' || !MAP[S.lang] ? en : tr(en, S.lang);
    if (out !== n.nodeValue) n.nodeValue = out; recs.set(n, { en, out });
  }
  const ATTRS = ['aria-label', 'title', 'placeholder'];
  function doAttrs(el) {
    if (el.closest && el.closest(SKIP.replace('input,', '').replace('textarea,', ''))) return; let m = arecs.get(el);
    for (const a of ATTRS) { if (!el.hasAttribute(a)) continue; const cur = el.getAttribute(a); const rec = m && m[a]; const en = rec && cur === rec.out ? rec.en : cur; const out = S.lang === 'en' || !MAP[S.lang] ? en : tr(en, S.lang); if (out !== cur) el.setAttribute(a, out); m = m || {}; m[a] = { en, out }; }
    if (m) arecs.set(el, m);
  }
  function walk(node) {
    if (!node) return; busy = true;
    try {
      if (node.nodeType === 3) { doText(node); return; } if (node.nodeType !== 1) return;
      const tw = document.createTreeWalker(node, NodeFilter.SHOW_TEXT); let n; while ((n = tw.nextNode())) doText(n);
      if (node.matches && node.matches('[aria-label],[title],[placeholder]')) doAttrs(node); node.querySelectorAll('[aria-label],[title],[placeholder]').forEach(doAttrs);
    } finally { busy = false; }
  }
  A.applyLang = () => {
    const l = MAP[S.lang] ? S.lang : 'en'; root.lang = l; root.dir = l === 'ar' ? 'rtl' : 'ltr';
    walk(document.body);
  };
  new MutationObserver((muts) => { if (busy || S.lang === 'en') return; for (const m of muts) { if (m.type === 'childList') m.addedNodes.forEach(walk); else if (m.type === 'characterData') walk(m.target); else if (m.type === 'attributes') { busy = true; try { doAttrs(m.target); } finally { busy = false; } } } }).observe(document.body, { childList: true, subtree: true, characterData: true, attributes: true, attributeFilter: ATTRS });
  A.tr = (s) => (S.lang === 'en' || !MAP[S.lang] ? s : tr(s, S.lang));
  A.langs = LANGS; A.langCount = D.length;

  /* ================= accessibility ================= */
  const applyA11y = () => {
    root.classList.toggle('rmotion', !!S.reduceMotion); if (!THUMB) root.classList.toggle('nofx', !!S.reduceMotion);
    if (S.highContrast) root.setAttribute('data-contrast', 'high'); else root.removeAttribute('data-contrast');
  };
  A.applyA11y = applyA11y;
  const skip = document.createElement('a'); skip.className = 'skip'; skip.href = '#stage'; skip.textContent = 'Skip to content'; document.body.prepend(skip);
  skip.addEventListener('click', (e) => { e.preventDefault(); const t = st.view === 'files' ? $('#paneMain') : $('#stage'); if (t) { t.setAttribute('tabindex', '-1'); t.focus(); } });
  const live = document.createElement('div'); live.id = 'srlive'; live.className = 'srOnly'; live.setAttribute('aria-live', 'polite'); live.setAttribute('role', 'status'); document.body.appendChild(live);
  const NAMES = { files: 'Files', servers: 'Servers', devices: 'Devices', transfers: 'Transfers', search: 'Search', tools: 'Tools', history: 'History', settings: 'Settings' };
  const go0 = A.go; A.go = function (v) { const r = go0.apply(this, arguments); live.textContent = A.tr(NAMES[v] || v); return r; };
  /* dialogs: the title names the dialog; ui.js keeps Tab inside and gives focus back */
  const dlg0 = A.dialog;
  A.dialog = function (o) {
    const c = dlg0.call(this, o); const d = c.el; d.tabIndex = -1;
    if (!d.contains(document.activeElement)) d.focus({ preventScroll: true });
    return c;
  };
  /* accessible names for icon-only buttons */
  const NAME_OF = { x: 'Close', 'x-circle': 'Remove', more: 'More actions', 'more-v': 'More actions', 'more-h': 'More actions', refresh: 'Refresh', 'arrow-left': 'Back', 'arrow-right': 'Forward', 'arrow-up': 'Up one folder', pause: 'Pause', play: 'Resume', search: 'Search', bell: 'Notifications', sun: 'Theme', moon: 'Theme', qr: 'Pair a phone', command: 'Command palette', grid: 'Grid view', list: 'List view', menu: 'Menu', star: 'Favorite', edit: 'Edit', trash: 'Delete', copy: 'Copy', plus: 'Add', minus: 'Remove', 'chevron-left': 'Previous', 'chevron-right': 'Next', 'chevron-down': 'Expand', 'chevron-up': 'Collapse', check: 'Confirm', link: 'Link', 'dots': 'More actions', eye: 'Show', settings: 'Settings', download: 'Download', upload: 'Upload', swap: 'Transfers' };
  const nameOf = (b) => (b.getAttribute('aria-label') || b.getAttribute('title') || b.textContent || '').trim() || (b.querySelector('input,select') && '') || '';
  A.a11yAudit = () => $$('button,[role="button"],[role="switch"],a[href],input:not([type="hidden"]),select,textarea').filter((b) => b.offsetParent !== null || b.closest('.dlg')).filter((b) => { if (b.tagName === 'INPUT' || b.tagName === 'SELECT' || b.tagName === 'TEXTAREA') { const l = b.id && $('label[for="' + b.id + '"]'); return !(b.getAttribute('aria-label') || l || b.closest('label') || b.placeholder); } return !nameOf(b); });
  let fieldSeq = 0;
  /* a field's visible label names it: <label> next to an input in a .fld is tied to it */
  const tieLabels = () => $$('.fld').forEach((f) => { const l = $('label', f), i = $('input,select,textarea', f); if (l && i && !l.htmlFor && !l.querySelector('input,select,textarea')) { if (!i.id) i.id = 'fld' + (++fieldSeq); l.htmlFor = i.id; } });
  A.a11yFix = () => { let n = 0; tieLabels(); A.a11yAudit().forEach((b) => { const svg = b.querySelector('svg[data-i]'); const key = b.dataset.act || b.dataset.pa || b.dataset.bk || (svg && svg.dataset.i) || ''; let nm = NAME_OF[key] || NAME_OF[(svg && svg.dataset.i) || ''] || ''; if (!nm && b.classList.contains('sw')) nm = (b.dataset.sw || b.dataset.sw2 || 'Toggle').replace(/([A-Z])/g, ' $1').toLowerCase().replace(/^./, (c) => c.toUpperCase()); if (!nm && key) nm = key.replace(/[-_.]/g, ' ').replace(/^./, (c) => c.toUpperCase()); if (nm) { b.setAttribute('aria-label', nm); n++; } }); return n; };
  new MutationObserver(() => { clearTimeout(A.a11yT); A.a11yT = setTimeout(() => { A.a11yFix(); }, 120); }).observe(document.body, { childList: true, subtree: true });
  /* switches get readable names */
  A.fixSw = () => fixSw();
  const fixSw = () => $$('.sw[role="switch"]').forEach((b) => { const row = b.closest('.sr'); const l = row && $('.l b', row); if (l && (!b.getAttribute('aria-label') || b.getAttribute('aria-label') === b.dataset.sw)) b.setAttribute('aria-label', l.textContent); });

  /* ================= settings group ================= */
  const se2 = A.settingsExtra;
  A.settingsExtra = (k) => {
    let h = se2(k); const row = k.row, seg = k.seg, sw = k.sw;
    const grp = '<div class="sg"><h3>Language and accessibility</h3>' + row('Language', '', '<div class="seg" data-set="lang">' + LANGS.map((l) => '<button data-v="' + l[0] + '" lang="' + l[0] + '" class="' + (S.lang === l[0] ? 'on' : '') + '">' + l[1] + '</button>').join('') + '</div>') +
      row('Reduce motion', '', sw('reduceMotion')) + row('High contrast', '', sw('highContrast')) + '</div>';
    h = h.replace('<div class="sg"><h3>Security</h3>', grp + '<div class="sg"><h3>Security</h3>');
    h = h.replace('<div class="sg"><h3>About</h3>', '<div class="sg"><h3>About</h3>' + row('Welcome tour', '', '<button class="btn" data-sx="welcome">Show again</button>'));
    return h;
  };
  const sa2 = A.settingsAction;
  A.settingsAction = (a) => { if (a === 'about') { A.aboutDialog(); return true; } if (a === 'welcome') { S.onboarded = false; A.save(); A.onboarding(1); return true; } return sa2(a); };
  $('#stage').addEventListener('click', (e) => { if (e.target.closest('[data-set="lang"] button,[data-set="textSize"] button,[data-sw="reduceMotion"],[data-sw="highContrast"]')) setTimeout(() => { A.applyLang(); applyA11y(); fixSw(); }, 0); });

  /* ================= About and support ================= */
  const uiContext = () => ['UI language: ' + S.lang + ' · theme ' + S.theme + ' · text ' + S.textSize + '%', 'Servers:'].concat(E.servers.map((s) => '  ' + s.name + ' · ' + s.state + (s.agent ? ' · agent ' + s.agent : '') + (s.latency ? ' · ' + s.latency + ' ms' : ''))).concat(['Unfinished transfers: ' + E.tasks.filter((t) => ['queued', 'running'].includes(t.state)).length, 'Paired devices (last loaded): ' + X.devices.length]).join('\n');
  /* The core writes the report (versions, OS, saved servers, recent log lines, no secrets); the page adds only what it knows. */
  const diagText = async () => { let core = ''; try { core = await E.call('diagnostics', {}); } catch (e) { core = 'Diagnostics from the core were not available: ' + (e && e.message ? e.message : e); } return core.trimEnd() + '\n\n' + uiContext(); };
  const copy = async (text, msg) => { try { await navigator.clipboard.writeText(text); A.snack(msg || 'Copied'); } catch (e) { A.snack('The clipboard is not available here', { error: true }); } };
  A.diagText = diagText;
  A.aboutDialog = () => {
    const v = A.appVersion ? A.appVersion() : '';
    A.dialog({ icon: 'plug', title: 'About RFE', width: 600, noFocus: true,
      body: '<div class="abt"><div class="logo">' + ic('plug', { size: 34 }) + '</div><div><b>RFE Desktop' + (v ? ' ' + esc(v) : '') + '</b><small>Remote File Explorer</small></div></div><div class="kv2"><div><b>License</b><small>GPL-3.0. Free software, source available.</small></div></div><div class="kv2"><div><b>How it connects</b><small>Talks to rfe over a pinned TLS connection. Files never pass through a third party.</small></div></div><div class="kv2"><div><b>Privacy</b><small>No analytics. Nothing leaves this computer except to the servers you add, and to GitHub when you check for updates.</small></div></div>',
      actions: [{ label: 'Close', kind: 'tx' }, { id: 'rep', label: 'Report a problem', kind: 'tx', icon: 'alert', cb: () => { setTimeout(A.reportDialog, 0); } }, { id: 'cp', label: 'Copy diagnostics', kind: 'f', icon: 'copy', cb: async () => { await copy(await diagText(), 'Diagnostics copied. No passwords or keys are included.'); return false; } }] });
  };
  A.reportDialog = () => {
    A.dialog({ icon: 'alert', title: 'Report a problem', width: 520, noFocus: true, body: '<p class="fxp" style="margin-top:0">Describe what happened. Nothing is sent from here: you copy the report and paste it into the issue tracker.</p><div class="fld"><label for="rpt">What went wrong?</label><textarea id="rpt" rows="5" style="width:100%;border:1px solid var(--outline);border-radius:.75rem;padding:.625rem;background:var(--surface);color:var(--on-surface);font:inherit"></textarea></div><label class="fxck"><input type="checkbox" id="rpd" checked> Include diagnostics (versions, servers and their state, no passwords)</label>',
      actions: [{ label: 'Cancel', kind: 'tx' }, { id: 'go', label: 'Copy report', kind: 'f', icon: 'copy', cb: async (c) => { const t = $('#rpt', c.el).value.trim(); if (!t) { $('#rpt', c.el).focus(); A.snack('Describe the problem first', { error: true }); return false; } await copy(t + ($('#rpd', c.el).checked ? '\n\n' + await diagText() : ''), 'Report copied. Paste it into a new issue.'); } }] });
  };

  /* ================= empty, error and offline states ================= */
  const apPrev = A.afterPage;
  A.afterPage = function (v) {
    if (apPrev) apPrev.apply(this, arguments);
    if (v === 'devices' && E.servers.length && E.servers.every((s) => s.state !== 'online')) {
      const stg = $('#stage'); if (!$('.nopc', stg)) { const b = document.createElement('div'); b.className = 'hint bad nopc'; b.setAttribute('role', 'alert'); b.style.margin = '0 0 .875rem'; b.innerHTML = ic('alert-circle') + '<span><b>No computer is online</b><br>Phones can only pair while a computer is connected. Existing pairings keep working when it comes back.</span><button class="btn sm" style="margin-left:auto" data-go-servers="1">Go to Servers</button>'; $('.ps', stg).after(b); b.querySelector('button').addEventListener('click', () => A.go('servers')); }
    }
    fixSw(); if (S.lang !== 'en') walk($('#stage'));
  };
  const tr0 = H['trash.restore'];
  H['trash.restore'] = () => {
    const rows = A.trashRows().filter((r) => X.trashSel.has(r.k)); const off = rows.filter((r) => !E.connected(r.t.host));
    if (off.length) { const nm = A.hostName(off[0].t.host); A.snack(nm + ' is offline. Reconnect to restore ' + (off.length === 1 ? '“' + off[0].n.n + '”' : off.length + ' items') + '.', { error: true, action: 'Servers', onAction: () => A.go('servers') }); return; }
    return tr0();
  };
  const sr0 = H['share.revoke'];
  H['share.revoke'] = (i) => {
    const s = X.shares[+i]; if (s && !E.connected(s.host)) { A.dialog({ icon: 'alert', title: 'Can’t revoke while offline', width: 460, body: '<div>The link for <b>' + esc(s.name) + '</b> is served by <b>' + esc(A.hostName(s.host)) + '</b>, which is not connected. The link stays active until you reconnect and revoke it. Anyone with the link can still use it until then.</div>', actions: [{ label: 'Close', kind: 'tx' }, { label: 'Go to Servers', kind: 'f', cb: () => { A.go('servers'); } }] }); return; }
    return sr0(i);
  };

  /* ================= palette, demos, boot ================= */
  A.paletteExtra.push((C) => {
    LANGS.forEach((l) => C('globe', 'Language: ' + l[1], () => { S.lang = l[0]; A.save(); A.applyLang(); if (st.view === 'settings') A.pageSettings(); }));
    C('info', 'About RFE', A.aboutDialog); C('alert', 'Report a problem…', A.reportDialog);
  });
  document.addEventListener('keydown', (e) => { if (e.key === 'F1' && !$('.scrim')) { e.preventDefault(); A.aboutDialog(); } });
  const boot = () => { applyA11y(); A.applyLang(); fixSw(); A.a11yFix(); };
  boot();
  document.addEventListener('DOMContentLoaded', () => {
    boot(); const q = new URLSearchParams(location.search); const demo = q.get('demo');
    const D2 = { about: () => A.aboutDialog(), report: () => A.reportDialog(), a11y: () => A.go('settings') };
    if (D2[demo]) { D2[demo](); if (THUMB) $$('.snack', $('#layer')).forEach((x) => x.remove()); A.applyLang(); }
  });
  A.fx5 = { tr, MAP, walk };
})();
