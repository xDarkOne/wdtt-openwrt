'use strict';
'require view';
'require form';

return view.extend({
	render: function () {
		var m, s, o;

		m = new form.Map('wdtt', _('WDTT — настройки'),
			_('Параметры подключения к серверу обхода. Меняются на лету — служба перезапустится при «Сохранить и применить».'));

		s = m.section(form.NamedSection, 'settings', 'wdtt');
		s.anonymous = true;

		o = s.option(form.Flag, 'enabled', _('Включить обход'));
		o.rmempty = false;

		o = s.option(form.ListValue, 'mode', _('Режим маршрутизации'));
		o.value('selective', _('Селективный — только заблокированное (рекомендуется)'));
		o.value('lan-all', _('Весь трафик LAN'));
		o.value('full', _('Весь роутер'));
		o.default = 'selective';

		o = s.option(form.Value, 'peer', _('Сервер (host:port)'));
		o.datatype = 'hostport';
		o.placeholder = 'your-server:56000';

		o = s.option(form.Value, 'password', _('Пароль владельца'));
		o.password = true;

		o = s.option(form.Value, 'hashes_url', _('URL ссылок на звонки (linkd)'));
		o.placeholder = 'http://your-server:56090/<token>/links?n=4';

		o = s.option(form.Value, 'call_slot', _('Слот звонков'),
			_('Какую непересекающуюся выборку звонков берёт этот роутер. Задай разные слоты на разных роутерах (0, 1, 2…), чтобы они не тянули одни и те же звонки. Пусто = как задано в URL (или случайная выборка).'));
		o.datatype = 'range(0,15)';
		o.placeholder = '0';

		o = s.option(form.Value, 'max_hashes', _('Число ссылок'), _('Сколько звонков использовать одновременно (больше = выше скорость).'));
		o.datatype = 'range(1,16)';
		o.default = '4';

		o = s.option(form.Value, 'workers', _('Воркеры'),
			_('9 воркеров на один хэш звонка. Чтобы задействовать все ссылки в параллель — ставь воркеры = (число ссылок × 9). 36 = 4 звонка.'));
		o.datatype = 'uinteger';
		o.default = '36';

		o = s.option(form.Value, 'mtu', _('MTU'));
		o.datatype = 'range(1200,1420)';
		o.default = '1280';

		o = s.option(form.Value, 'refresh', _('Интервал обновления'), _('Как часто обновлять ссылки и пересобирать сессию (напр. 15m).'));
		o.placeholder = '15m';

		o = s.option(form.Value, 'device_id', _('ID устройства'), _('Назначается автоматически; сервер выдаёт по нему адрес.'));
		o.readonly = true;

		s = m.section(form.NamedSection, 'settings', 'wdtt', _('Авто-failover с NetShift'),
			_('Держать WDTT в спячке, пока работает NetShift, и автоматически поднимать его, ' +
			  'когда аплинк NetShift (AmneziaWG) падает — например при включении белых списков.'));
		s.anonymous = true;

		o = s.option(form.Flag, 'failover', _('Включить авто-failover'));
		o.default = '0';
		o.rmempty = false;

		o = s.option(form.Value, 'netshift_iface', _('Интерфейс аплинка NetShift'),
			_('Ядровый WireGuard-интерфейс NetShift (обычно AWG).'));
		o.default = 'AWG';
		o.depends('failover', '1');

		o = s.option(form.Value, 'netshift_stale', _('Порог «протух»'),
			_('Если последний handshake старше этого — NetShift считается упавшим (напр. 180s).'));
		o.default = '180s';
		o.depends('failover', '1');

		s = m.section(form.NamedSection, 'settings', 'wdtt', _('Обновление списков'),
			_('Периодически подтягивать свежие списки с GitHub, как в podkop.'));
		s.anonymous = true;

		o = s.option(form.Flag, 'auto_update', _('Авто-обновление списков'),
			_('Раз в сутки перекачивать выбранные списки и пересобирать набор.'));
		o.default = '1';
		o.rmempty = false;

		o = s.option(form.Value, 'auto_update_hour', _('Час обновления (0–23)'));
		o.datatype = 'range(0,23)';
		o.default = '5';
		o.depends('auto_update', '1');

		o = s.option(form.ListValue, 'lists_via_tunnel', _('Если GitHub недоступен'),
			_('Тянуть списки через VK-туннель, когда прямой доступ к GitHub закрыт (белый список).'));
		o.value('auto', _('Авто — сначала напрямую, потом через туннель'));
		o.value('always', _('Всегда через туннель'));
		o.value('never', _('Только напрямую'));
		o.default = 'auto';

		s = m.section(form.NamedSection, 'settings', 'wdtt', _('Защита от утечек'),
			_('Чтобы обход не «протекал» мимо туннеля. Применяется при «Сохранить и обновить списки».'));
		s.anonymous = true;

		o = s.option(form.Flag, 'block_doh', _('Блокировать DoH/DoT'),
			_('Закрыть внешние DNS-over-HTTPS/TLS, чтобы клиенты резолвили через роутер — иначе домены обхода не попадут в набор. Списки: dibdot/DoH-IP-blocklists.'));
		o.default = '0';
		o.rmempty = false;

		o = s.option(form.Flag, 'block_ipv6', _('Защита от IPv6-утечки'),
			_('Дропать IPv6 к доменам обхода → клиент падает на IPv4, который несёт туннель. Остальной IPv6 не трогается.'));
		o.default = '0';
		o.rmempty = false;

		return m.render();
	}
});
