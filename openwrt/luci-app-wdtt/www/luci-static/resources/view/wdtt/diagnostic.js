'use strict';
'require view';
'require rpc';
'require ui';
'require poll';

var callDiag   = rpc.declare({ object: 'wdtt', method: 'diag' });
var callLogs   = rpc.declare({ object: 'wdtt', method: 'logs' });
var callAction = rpc.declare({ object: 'wdtt', method: 'action', params: [ 'action' ] });

function badge(ok, y, n) {
	return E('span', {
		style: 'padding:2px 10px;border-radius:12px;color:#fff;background:' + (ok ? '#3aa655' : '#c0392b')
	}, ok ? y : n);
}
function row(k, v) {
	return E('tr', { 'class': 'tr' }, [
		E('td', { 'class': 'td', style: 'width:50%;color:#666' }, k),
		E('td', { 'class': 'td' }, v)
	]);
}

return view.extend({
	load: function () { return callDiag(); },

	render: function (d) {
		var table = E('table', { 'class': 'table' });

		function draw(x) {
			x = x || {};
			table.innerHTML = '';
			table.appendChild(row(_('Сервер доступен') + (x.server_host ? ' (' + x.server_host + ')' : ''),
				x.server_reachable ? badge(true, (x.server_ping ? x.server_ping + ' ms' : _('да')), '') : badge(false, '', _('нет'))));
			table.appendChild(row(_('Выход через туннель'),
				x.exit_ok ? E('span', {}, '✓ ' + (x.exit_ip || '')) : badge(false, '', _('нет / туннель спит'))));
			table.appendChild(row(_('DNS резолвит'), x.dns_ok ? badge(true, _('да'), '') : badge(false, '', _('нет'))));
			table.appendChild(row(_('IP в наборе обхода (v4 / v6)'), String(x.bypass_ips || 0) + ' / ' + String(x.bypass_ips6 || 0)));
			table.appendChild(row(_('Заблокировано DoH-адресов'), String(x.doh_ips || 0)));
		}
		draw(d);

		var logBox = E('pre', {
			style: 'max-height:340px;overflow:auto;background:#0d0d0d;color:#cfcfcf;padding:10px;border-radius:6px;font-size:85%;white-space:pre-wrap'
		}, _('загрузка лога…'));
		function drawLog(r) {
			logBox.textContent = (r && r.log) ? r.log : _('(лог пуст)');
		}
		callLogs().then(drawLog);
		poll.add(function () { return callLogs().then(drawLog); }, 5);

		var mkBtn = function (label, cls, fn) {
			return E('button', { 'class': 'btn cbi-button ' + (cls || ''), 'click': ui.createHandlerFn(this, fn) }, label);
		};
		var btnDiag = mkBtn(_('Перепроверить'), 'cbi-button-action', function (ev) {
			var b = ev.target; b.disabled = true;
			return callDiag().then(draw).finally(function () { b.disabled = false; });
		});
		var btnRebuild = mkBtn(_('Пересобрать списки'), '', function (ev) {
			var b = ev.target; b.disabled = true;
			return callAction('reload_lists').then(function (r) {
				ui.addNotification(null, E('p', (r && r.ok) ? _('Списки пересобраны.') : ((r && r.error) || _('Ошибка.'))), (r && r.ok) ? 'info' : 'warning');
			}).finally(function () { b.disabled = false; });
		});
		var btnRestart = mkBtn(_('Перезапустить службу'), '', function (ev) {
			var b = ev.target; b.disabled = true;
			return callAction('restart').then(function () {
				ui.addNotification(null, E('p', _('Служба перезапущена.')), 'info');
			}).finally(function () { b.disabled = false; });
		});

		return E('div', {}, [
			E('h2', {}, _('WDTT — диагностика')),
			E('div', { 'class': 'cbi-section' }, [
				E('h3', {}, _('Проверки')),
				table,
				E('div', { 'class': 'cbi-value', style: 'margin-top:10px;display:flex;gap:8px;flex-wrap:wrap' },
					[ btnDiag, btnRebuild, btnRestart ])
			]),
			E('div', { 'class': 'cbi-section' }, [
				E('h3', {}, _('Журнал (live)')),
				logBox
			])
		]);
	},

	handleSave: null,
	handleSaveApply: null,
	handleReset: null
});
