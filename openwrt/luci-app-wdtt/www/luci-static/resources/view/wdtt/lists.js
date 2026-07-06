'use strict';
'require view';
'require form';
'require rpc';
'require ui';
'require uci';

var callAction = rpc.declare({ object: 'wdtt', method: 'action', params: [ 'action' ] });

// 1:1 from itdoginfo/podkop (DOMAIN_LIST_OPTIONS).
var DOMAIN_LIST_OPTIONS = {
	russia_inside:  'Russia inside',
	russia_outside: 'Russia outside',
	ukraine_inside: 'Ukraine',
	geoblock:       'Geo Block',
	block:          'Block',
	porn:           'Porn',
	news:           'News',
	anime:          'Anime',
	youtube:        'Youtube',
	discord:        'Discord',
	meta:           'Meta',
	twitter:        'Twitter (X)',
	hdrezka:        'HDRezka',
	tiktok:         'Tik-Tok',
	telegram:       'Telegram',
	cloudflare:     'Cloudflare',
	google_ai:      'Google AI',
	google_play:    'Google Play',
	hodca:          'H.O.D.C.A',
	roblox:         'Roblox',
	hetzner:        'Hetzner ASN',
	ovh:            'OVH ASN',
	digitalocean:   'Digital Ocean ASN',
	cloudfront:     'CloudFront ASN'
};
var REGIONAL_OPTIONS = [ 'russia_inside', 'russia_outside', 'ukraine_inside' ];
var ALLOWED_WITH_RUSSIA_INSIDE = [
	'russia_inside', 'meta', 'twitter', 'discord', 'telegram', 'cloudflare',
	'google_ai', 'google_play', 'hetzner', 'ovh', 'hodca', 'roblox',
	'digitalocean', 'cloudfront'
];

// Mirror podkop: only one regional list, and russia_inside excludes the
// services it already contains.
function enforceExclusions(values) {
	var vals = Array.isArray(values) ? values.slice() : (values ? [ values ] : []);
	var regionals = vals.filter(function (v) { return REGIONAL_OPTIONS.indexOf(v) >= 0; });
	if (regionals.length > 1) {
		var keep = regionals[regionals.length - 1]; // last selected wins
		vals = vals.filter(function (v) { return REGIONAL_OPTIONS.indexOf(v) < 0 || v === keep; });
	}
	if (vals.indexOf('russia_inside') >= 0) {
		vals = vals.filter(function (v) { return ALLOWED_WITH_RUSSIA_INSIDE.indexOf(v) >= 0; });
	}
	return vals;
}

return view.extend({
	render: function () {
		var m, s, o;

		m = new form.Map('wdtt', _('Списки обхода'),
			_('Что гнать через VK-туннель. Всё остальное идёт напрямую через WAN. ' +
			  'Списки: itdoginfo/allow-domains. После изменений нажми «Сохранить и обновить списки».'));

		s = m.section(form.NamedSection, 'settings', 'wdtt');
		s.anonymous = true;

		o = s.option(form.DynamicList, 'community_list', _('Сервисы (community lists)'),
			_('Выбери сервис и добавь. «Russia inside» уже включает многие сервисы — дубли снимаются автоматически; региональный список можно только один.'));
		Object.keys(DOMAIN_LIST_OPTIONS).forEach(function (k) { o.value(k, DOMAIN_LIST_OPTIONS[k]); });
		o.onchange = function (ev, section_id, values) {
			var cur = Array.isArray(values) ? values : (values ? [ values ] : []);
			var filtered = enforceExclusions(cur);
			if (filtered.length !== cur.length) {
				var el = this.getUIElement(section_id);
				if (el) el.setValue(filtered);
				ui.addNotification(null,
					E('p', _('Убраны дублирующие/несовместимые списки (как в podkop).')), 'info');
			}
		};

		o = s.option(form.Flag, 'use_zapret', _('Мой список zapret'),
			_('Домены из /opt/zapret/ipset/zapret-hosts-user.txt.'));
		o.default = '1';
		o.rmempty = false;

		o = s.option(form.TextValue, 'custom_domains', _('Свои домены'),
			_('По одному в строке или через запятую/пробел. Комментарии — через //.'));
		o.rows = 6;
		o.monospace = true;

		o = s.option(form.DynamicList, 'local_domain_list', _('Локальные списки доменов'),
			_('Пути к файлам со списком доменов на роутере (напр. /opt/zapret/ipset/my.txt).'));
		o = s.option(form.DynamicList, 'remote_domain_list', _('Удалённые списки доменов'),
			_('URL-ы, откуда качать списки доменов (по строке домен).'));

		o = s.option(form.TextValue, 'custom_subnets', _('Свои подсети / IP'),
			_('CIDR или IP назначения, которые гнать через туннель (напр. 149.154.160.0/20).'));
		o.rows = 4;
		o.monospace = true;

		o = s.option(form.DynamicList, 'local_subnet_list', _('Локальные списки подсетей'),
			_('Пути к файлам со списком подсетей на роутере.'));
		o = s.option(form.DynamicList, 'remote_subnet_list', _('Удалённые списки подсетей'),
			_('URL-ы, откуда качать списки подсетей.'));

		o = s.option(form.DynamicList, 'fully_routed_ip', _('Полностью в туннель (по устройству)'),
			_('Локальные IP/подсети, весь трафик которых всегда идёт через туннель (напр. 192.168.1.50).'));

		return m.render().then(function (node) {
			var btn = E('button', {
				'class': 'btn cbi-button cbi-button-apply',
				'click': ui.createHandlerFn(this, function (ev) {
					var b = ev.target; b.disabled = true;
					return m.save()
						.then(function () { return uci.apply(); })
						.then(function () { return callAction('reload_lists'); })
						.then(function (res) {
							if (res && res.ok)
								ui.addNotification(null, E('p', _('Списки пересобраны, dnsmasq и файрвол перезагружены.')), 'info');
							else
								ui.addNotification(null, E('p', (res && res.error) || _('Не удалось обновить списки.')), 'warning');
						})
						.catch(function (e) { ui.addNotification(null, E('p', _('Ошибка: ') + e), 'error'); })
						.finally(function () { b.disabled = false; });
				})
			}, _('Сохранить и обновить списки'));
			node.appendChild(E('div', { 'class': 'cbi-page-actions' }, [ btn ]));
			return node;
		});
	},

	handleSaveApply: null,
	handleSave: null,
	handleReset: null
});
