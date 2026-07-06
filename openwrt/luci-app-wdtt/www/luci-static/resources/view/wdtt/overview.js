'use strict';
'require view';
'require rpc';
'require ui';
'require poll';

var callStatus = rpc.declare({ object: 'wdtt', method: 'status' });
var callCheck  = rpc.declare({ object: 'wdtt', method: 'check' });
var callAction = rpc.declare({ object: 'wdtt', method: 'action', params: [ 'action' ] });

function badge(ok, textOk, textNo) {
	return E('span', {
		style: 'padding:2px 10px;border-radius:12px;color:#fff;font-size:90%;background:' + (ok ? '#3aa655' : '#8a8a8a')
	}, ok ? textOk : textNo);
}

function row(k, v) {
	return E('tr', { 'class': 'tr' }, [
		E('td', { 'class': 'td', style: 'width:45%;color:#666' }, k),
		E('td', { 'class': 'td' }, v)
	]);
}

function fmtAge(a) {
	if (a == null || a < 0) return '—';
	if (a < 120) return a + ' ' + _('с назад');
	return Math.round(a / 60) + ' ' + _('мин назад');
}

function fmtBytes(b) {
	b = Number(b) || 0;
	var u = [ 'B', 'KiB', 'MiB', 'GiB', 'TiB' ], i = 0;
	while (b >= 1024 && i < u.length - 1) { b /= 1024; i++; }
	return (i ? b.toFixed(2) : b) + ' ' + u[i];
}

return view.extend({
	load: function () { return callStatus(); },

	render: function (st) {
		var statusTable = E('table', { 'class': 'table' });

		function draw(s) {
			s = s || {};
			statusTable.innerHTML = '';
			statusTable.appendChild(row(_('Обход включён'), badge(!!s.enabled, _('да'), _('нет'))));
			statusTable.appendChild(row(_('Служба'), badge(!!s.running, _('работает'), _('остановлена'))));
			statusTable.appendChild(row(_('Туннель'), s.up ? badge(true, _('поднят'), '') :
				(s.failover ? E('span', {}, _('спит (failover)')) : badge(false, '', _('нет')))));
			statusTable.appendChild(row(_('Адрес в туннеле'), s.addr || '—'));
			statusTable.appendChild(row(_('Последний handshake'), fmtAge(s.handshake_age)));
			statusTable.appendChild(row(_('Трафик (↓ / ↑)'), fmtBytes(s.rx_bytes) + ' / ' + fmtBytes(s.tx_bytes)));
			statusTable.appendChild(row(_('Режим'), (s.mode || '—') + (s.failover ? ' + failover' : '')));
			statusTable.appendChild(row(_('Сервер'), s.peer || '—'));
			statusTable.appendChild(row(_('Доменов / IP в обходе'),
				String(s.bypass_domains || 0) + ' / ' + String(s.bypass_ips || 0) +
				(s.block_ipv6 ? (' (+' + String(s.bypass_ips6 || 0) + ' IPv6)') : '')));
			statusTable.appendChild(row(_('Защита'),
				(s.block_doh ? _('DoH ✓ ') : _('DoH ✗ ')) + (s.block_ipv6 ? _('IPv6 ✓') : _('IPv6 ✗'))));
		}
		draw(st);

		poll.add(function () {
			return callStatus().then(draw);
		}, 5);

		function act(a, btn) {
			btn.classList.add('spinning'); btn.disabled = true;
			return callAction(a)
				.then(callStatus)
				.then(draw)
				.finally(function () { btn.classList.remove('spinning'); btn.disabled = false; });
		}

		var mkBtn = function (label, cls, handler) {
			return E('button', {
				'class': 'btn cbi-button ' + (cls || ''),
				'click': ui.createHandlerFn(this, handler)
			}, label);
		};

		var btnStart   = mkBtn(_('Включить'),       'cbi-button-apply', function (ev) { return act('start', ev.target); });
		var btnStop    = mkBtn(_('Выключить'),      'cbi-button-reset', function (ev) { return act('stop', ev.target); });
		var btnRestart = mkBtn(_('Перезапустить'),  '',                 function (ev) { return act('restart', ev.target); });
		var btnReload  = mkBtn(_('Обновить списки'),'',                 function (ev) { return act('reload_lists', ev.target); });

		var checkOut = E('span', { style: 'margin-left:12px;color:#333' }, '');
		var btnCheck = mkBtn(_('Проверить выход'), '', function (ev) {
			var b = ev.target; b.disabled = true; checkOut.textContent = _('проверяю…');
			return callCheck().then(function (r) {
				checkOut.textContent = (r && r.ok)
					? (_('внешний IP через туннель: ') + r.exit_ip)
					: _('туннель не отвечает');
			}).finally(function () { b.disabled = false; });
		});

		return E('div', {}, [
			E('h2', {}, _('WDTT — обход белых списков через звонки VK')),
			E('p', { 'class': 'cbi-section-descr' },
				_('Заворачивает только заблокированные ресурсы через VK-звонки. Остальной трафик идёт напрямую.')),
			E('div', { 'class': 'cbi-section' }, [
				E('h3', {}, _('Состояние')),
				statusTable
			]),
			E('div', { 'class': 'cbi-section' }, [
				E('h3', {}, _('Управление')),
				E('div', { 'class': 'cbi-value', style: 'display:flex;gap:8px;flex-wrap:wrap' },
					[ btnStart, btnStop, btnRestart, btnReload ]),
				E('div', { 'class': 'cbi-value', style: 'margin-top:10px' }, [ btnCheck, checkOut ])
			])
		]);
	},

	handleSave: null,
	handleSaveApply: null,
	handleReset: null
});
