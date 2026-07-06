'use strict';
'require view';
'require form';
'require ui';

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

// Mirror podkop (section.js) exactly: only one regional list, and russia_inside
// excludes the services it already contains. Podkop derives the kept regional
// by REGIONAL_OPTIONS order (filter the constant by what's selected, keep the
// last), so replicate that ordering — not the user's selection order.
function enforceExclusions(values) {
	var vals = Array.isArray(values) ? values.slice() : (values ? [ values ] : []);
	var regionals = REGIONAL_OPTIONS.filter(function (v) { return vals.indexOf(v) >= 0; });
	if (regionals.length > 1) {
		var keep = regionals[regionals.length - 1];
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
			  'Списки: itdoginfo/allow-domains. После «Сохранить и применить» набор ' +
			  'пересобирается автоматически (домены резолвятся, подсети сервисов ' +
			  'вроде Telegram берутся напрямую).'));

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

		// Standard Save & Apply / Save / Reset. On apply, procd reloads the
		// wdtt-client service (reload_service), which rebuilds the bypass lists
		// (wdtt-genlists) — so saving here re-resolves the sets automatically.
		return m.render();
	}
});
