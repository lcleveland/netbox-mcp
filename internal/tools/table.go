package tools

// Resources is the first-class tool table. Everything else is reachable via
// netbox_api.
var Resources = []Resource{
	// ipam
	{Name: "netbox_prefix", Group: "ipam", Path: "/api/ipam/prefixes/", Title: "IP prefixes",
		Description: "IPv4/IPv6 prefixes (subnets). Filters: prefix, q, within, within_include, contains, vrf_id, site_id, vlan_id, status, role, tenant, tag, family (4|6), mask_length. " +
			"For free space inside a prefix use netbox_available."},
	{Name: "netbox_ip_address", Group: "ipam", Path: "/api/ipam/ip-addresses/", Title: "IP addresses",
		Description: "Individual IP addresses (with mask). Filters: address, q, parent (prefix), vrf_id, device, device_id, virtual_machine, interface_id, status, role, dns_name, tenant, tag. " +
			"To find which device has an IP, list with address and read assigned_object."},
	{Name: "netbox_ip_range", Group: "ipam", Path: "/api/ipam/ip-ranges/", Title: "IP ranges",
		Description: "Ranges of IP addresses (start_address..end_address). Filters: q, start_address, end_address, contains, vrf_id, status, role, tenant, tag."},
	{Name: "netbox_vlan", Group: "ipam", Path: "/api/ipam/vlans/", Title: "VLANs",
		Description: "VLANs. Filters: vid, name, q, group_id, site_id, status, role, tenant, tag."},
	{Name: "netbox_vlan_group", Group: "ipam", Path: "/api/ipam/vlan-groups/", Title: "VLAN groups",
		Description: "VLAN groups (VLAN ID pools scoped to a site, location, rack, etc.). Filters: name, slug, q, scope_type, scope_id, tag."},
	{Name: "netbox_vrf", Group: "ipam", Path: "/api/ipam/vrfs/", Title: "VRFs",
		Description: "VRFs (routing tables). Filters: name, rd, q, tenant, tag."},

	// dcim
	{Name: "netbox_site", Group: "dcim", Path: "/api/dcim/sites/", Title: "Sites",
		Description: "Sites (buildings/campuses). Filters: name, slug, q, region, group, status, tenant, tag."},
	{Name: "netbox_location", Group: "dcim", Path: "/api/dcim/locations/", Title: "Locations",
		Description: "Locations within a site (floors, rooms; nested). Filters: name, slug, q, site_id, parent_id, status, tenant, tag."},
	{Name: "netbox_rack", Group: "dcim", Path: "/api/dcim/racks/", Title: "Racks",
		Description: "Equipment racks. Filters: name, q, site_id, location_id, status, role, tenant, tag."},
	{Name: "netbox_device", Group: "dcim", Path: "/api/dcim/devices/", Title: "Devices",
		Description: "Physical devices (switches, servers, firewalls...). Filters: name, q, site_id, location_id, rack_id, role, manufacturer, device_type, platform, serial, asset_tag, status, primary_ip4, tenant, tag. " +
			"Creating a device needs name, device_type, role and site ids."},
	{Name: "netbox_interface", Group: "dcim", Path: "/api/dcim/interfaces/", Title: "Device interfaces",
		Description: "Interfaces on devices. Filters: device, device_id, name, q, type, enabled, mgmt_only, mac_address, vlan_id, cabled, connected, tag."},
	{Name: "netbox_cable", Group: "dcim", Path: "/api/dcim/cables/", Title: "Cables",
		Description: "Cables between interfaces, ports and circuits. Filters: device, device_id, site_id, rack_id, type, status, label, color, tag. " +
			"a_terminations/b_terminations are lists of {object_type, object_id}."},
	{Name: "netbox_manufacturer", Group: "dcim", Path: "/api/dcim/manufacturers/", Title: "Manufacturers",
		Description: "Hardware manufacturers (vendors). Filters: name, slug, q, tag."},
	{Name: "netbox_device_type", Group: "dcim", Path: "/api/dcim/device-types/", Title: "Device types",
		Description: "Device models (make and model). Filters: model, slug, part_number, q, manufacturer, manufacturer_id, u_height, subdevice_role, airflow, tag. " +
			"Creating a device type needs manufacturer id, model and slug."},
	{Name: "netbox_device_role", Group: "dcim", Path: "/api/dcim/device-roles/", Title: "Device roles",
		Description: "Device roles (core switch, firewall, server...; nested). Filters: name, slug, q, color, vm_role, parent_id, tag."},
	{Name: "netbox_platform", Group: "dcim", Path: "/api/dcim/platforms/", Title: "Platforms",
		Description: "Platforms (operating systems such as IOS, Junos, Windows; nested). Filters: name, slug, q, manufacturer, manufacturer_id, parent_id, tag."},
	{Name: "netbox_module", Group: "dcim", Path: "/api/dcim/modules/", Title: "Modules",
		Description: "Modules installed in device module bays (line cards, PSUs, transceivers). Filters: q, device, device_id, site_id, module_type, module_type_id, module_bay_id, manufacturer, serial, asset_tag, status, tag. " +
			"Creating a module needs device, module_bay and module_type ids."},
	{Name: "netbox_inventory_item", Group: "dcim", Path: "/api/dcim/inventory-items/", Title: "Inventory items",
		Description: "Inventory items inside devices (parts, licences, optics; nested). Filters: name, q, device, device_id, site_id, manufacturer, role, serial, asset_tag, part_id, status, discovered, parent_id, tag. " +
			"Creating one needs device id and name."},
	{Name: "netbox_mac_address", Group: "dcim", Path: "/api/dcim/mac-addresses/", Title: "MAC addresses",
		Description: "MAC addresses, assigned to device or VM interfaces. Filters: mac_address, q, device, device_id, virtual_machine, virtual_machine_id, interface_id, vminterface_id, assigned, primary, tag. " +
			"To find which interface has a MAC, list with mac_address and read assigned_object."},
	{Name: "netbox_virtual_chassis", Group: "dcim", Path: "/api/dcim/virtual-chassis/", Title: "Virtual chassis",
		Description: "Virtual chassis (switch stacks); member devices carry virtual_chassis and vc_position. Filters: name, domain, q, site_id, master_id, tenant, tag."},

	// circuits
	{Name: "netbox_provider", Group: "circuits", Path: "/api/circuits/providers/", Title: "Circuit providers",
		Description: "Circuit providers (ISPs, carriers). Filters: name, slug, q, asn, site_id, tag."},
	{Name: "netbox_provider_account", Group: "circuits", Path: "/api/circuits/provider-accounts/", Title: "Provider accounts",
		Description: "Accounts held with a provider. Filters: name, account, q, provider, provider_id, tag. Creating one needs provider id and account."},
	{Name: "netbox_circuit_type", Group: "circuits", Path: "/api/circuits/circuit-types/", Title: "Circuit types",
		Description: "Circuit types (internet, MPLS, dark fibre...). Filters: name, slug, q, color, tag."},
	{Name: "netbox_circuit", Group: "circuits", Path: "/api/circuits/circuits/", Title: "Circuits",
		Description: "Provider circuits (WAN/internet links). Filters: cid, q, provider, provider_id, type, type_id, status, site_id, location_id, tenant, install_date, tag. " +
			"Creating a circuit needs cid, provider and type ids. Where it lands is in its A/Z terminations (netbox_circuit_termination)."},
	{Name: "netbox_circuit_termination", Group: "circuits", Path: "/api/circuits/circuit-terminations/", Title: "Circuit terminations",
		Description: "The A and Z ends of circuits. Filters: circuit_id, term_side (A|Z), q, site_id, location_id, provider_id, provider_network_id, xconnect_id, cabled, tag. " +
			"termination_type/termination_id point at a site, location, region, site group or provider network."},

	// tenancy
	{Name: "netbox_tenant", Group: "tenancy", Path: "/api/tenancy/tenants/", Title: "Tenants",
		Description: "Tenants (customers or departments that own objects). Filters: name, slug, q, group, tag."},

	// virtualization
	{Name: "netbox_virtual_machine", Group: "virtualization", Path: "/api/virtualization/virtual-machines/", Title: "Virtual machines",
		Description: "Virtual machines. Filters: name, q, cluster_id, site_id, role, platform, status, primary_ip4, tenant, tag."},
	{Name: "netbox_vm_interface", Group: "virtualization", Path: "/api/virtualization/interfaces/", Title: "VM interfaces",
		Description: "Interfaces on virtual machines. Filters: virtual_machine, virtual_machine_id, name, q, enabled, mac_address, vlan_id, tag."},

	// extras
	{Name: "netbox_journal_entry", Group: "extras", Path: "/api/extras/journal-entries/", Title: "Journal entries",
		Description: "Free-text journal notes attached to any object: why something changed, maintenance notes. Filters: assigned_object_type (e.g. dcim.device), assigned_object_id, kind, created_by, created_after, tag. " +
			"Create needs assigned_object_type, assigned_object_id, comments (markdown) and optional kind (info|success|warning|danger)."},
	{Name: "netbox_tag", Group: "extras", Path: "/api/extras/tags/", Title: "Tags",
		Description: "Tags that can be applied to objects. Filters: name, slug, q, color, for_object_type_id. Objects reference tags by id or {\"name\": ...}; the tag must exist first."},
	{Name: "netbox_object_change", Group: "extras", Path: "/api/core/object-changes/", Title: "Changelog", ReadOnly: true,
		Description: "The NetBox changelog: who changed what, when, and why (message). Filters: user, user_name, request_id, action (create|update|delete), changed_object_type (e.g. ipam.prefix), changed_object_id, time_after, time_before, q. " +
			"Every write through this server returns a request_id to look up here."},
	{Name: "netbox_custom_field", Group: "extras", Path: "/api/extras/custom-fields/", Title: "Custom field definitions", ReadOnly: true,
		Description: "Definitions of custom fields: name, type, which object types they apply to, choices and validation. Filters: name, q, type, object_type (e.g. dcim.device), required. " +
			"Look these up before writing custom_fields on an object; unknown names are rejected."},
}
