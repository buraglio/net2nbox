// Package netbox provides a thin client for the NetBox REST API (v3/v4 compatible).
// Authentication uses the standard token header:
//
//	Authorization: Token <token>
//
// All write operations respect a dry-run flag that logs intent without calling the API.
package netbox

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Client is a minimal NetBox REST API client.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
	DryRun     bool
	Log        *slog.Logger
}

// New creates a NetBox client. baseURL should be the root URL, e.g. "https://netbox.example.com".
func New(baseURL, token string, dryRun bool) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		DryRun: dryRun,
		Log:    slog.Default(),
	}
}

type Manufacturer struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type DeviceType struct {
	ID           int          `json:"id"`
	Manufacturer Manufacturer `json:"manufacturer"`
	Model        string       `json:"model"`
	Slug         string       `json:"slug"`
}

type DeviceRole struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type Site struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type Device struct {
	ID           int         `json:"id"`
	Name         string      `json:"name"`
	Serial       string      `json:"serial"`
	DeviceType   DeviceType  `json:"device_type"`
	Role         DeviceRole  `json:"role"`
	Site         Site        `json:"site"`
	PrimaryIPv4  *IPAddress  `json:"primary_ip4"`
	PrimaryIPv6  *IPAddress  `json:"primary_ip6"`
}

type Interface struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
	MACAddress  string `json:"mac_address"`
	MTU         *int   `json:"mtu"`
	Type        struct {
		Value string `json:"value"`
	} `json:"type"`
	Device struct {
		ID int `json:"id"`
	} `json:"device"`
}

type IPAddress struct {
	ID                 int    `json:"id"`
	Address            string `json:"address"`
	Description        string `json:"description"`
	AssignedObjectType string `json:"assigned_object_type"`
	AssignedObjectID   *int   `json:"assigned_object_id"`
	Family             struct {
		Value int `json:"value"`
	} `json:"family"`
}

type listResponse[T any] struct {
	Count   int `json:"count"`
	Results []T `json:"results"`
}

func (c *Client) get(path string, query url.Values) ([]byte, error) {
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	c.setHeaders(req)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", u, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("GET %s → %d: %s", u, resp.StatusCode, body)
	}
	return body, nil
}

func (c *Client) post(path string, payload any) ([]byte, error) {
	return c.writeRequest(http.MethodPost, path, payload)
}

func (c *Client) patch(path string, payload any) ([]byte, error) {
	return c.writeRequest(http.MethodPatch, path, payload)
}

func (c *Client) writeRequest(method, path string, payload any) ([]byte, error) {
	u := c.baseURL + path
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	if c.DryRun {
		c.Log.Info("dry-run: would send", "method", method, "url", u, "body", string(data))
		return []byte("{}"), nil
	}
	req, err := http.NewRequest(method, u, strings.NewReader(string(data)))
	if err != nil {
		return nil, err
	}
	c.setHeaders(req)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", method, u, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s %s → %d: %s", method, u, resp.StatusCode, body)
	}
	return body, nil
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Token "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
}

// FindOrCreateManufacturer returns an existing manufacturer by name or creates one.
func (c *Client) FindOrCreateManufacturer(name string) (*Manufacturer, error) {
	q := url.Values{"name": {name}}
	body, err := c.get("/api/dcim/manufacturers/", q)
	if err != nil {
		return nil, err
	}
	var result listResponse[Manufacturer]
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	if result.Count > 0 {
		m := result.Results[0]
		c.Log.Debug("found manufacturer", "name", name, "id", m.ID)
		return &m, nil
	}
	c.Log.Info("creating manufacturer", "name", name)
	payload := map[string]string{"name": name, "slug": slugify(name)}
	body, err = c.post("/api/dcim/manufacturers/", payload)
	if err != nil {
		return nil, fmt.Errorf("create manufacturer %q: %w", name, err)
	}
	if c.DryRun {
		return &Manufacturer{Name: name, Slug: slugify(name)}, nil
	}
	var m Manufacturer
	return &m, json.Unmarshal(body, &m)
}

// FindOrCreateDeviceType returns an existing device type or creates one.
func (c *Client) FindOrCreateDeviceType(manufacturerID int, model string) (*DeviceType, error) {
	q := url.Values{"manufacturer_id": {fmt.Sprint(manufacturerID)}, "model": {model}}
	body, err := c.get("/api/dcim/device-types/", q)
	if err != nil {
		return nil, err
	}
	var result listResponse[DeviceType]
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	if result.Count > 0 {
		dt := result.Results[0]
		c.Log.Debug("found device-type", "model", model, "id", dt.ID)
		return &dt, nil
	}
	c.Log.Info("creating device-type", "model", model)
	payload := map[string]any{
		"manufacturer": manufacturerID,
		"model":        model,
		"slug":         slugify(model),
	}
	body, err = c.post("/api/dcim/device-types/", payload)
	if err != nil {
		return nil, fmt.Errorf("create device-type %q: %w", model, err)
	}
	if c.DryRun {
		return &DeviceType{Model: model}, nil
	}
	var dt DeviceType
	return &dt, json.Unmarshal(body, &dt)
}

// FindOrCreateDeviceRole returns an existing role by name or creates one.
func (c *Client) FindOrCreateDeviceRole(name string) (*DeviceRole, error) {
	q := url.Values{"name": {name}}
	body, err := c.get("/api/dcim/device-roles/", q)
	if err != nil {
		return nil, err
	}
	var result listResponse[DeviceRole]
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	if result.Count > 0 {
		r := result.Results[0]
		return &r, nil
	}
	c.Log.Info("creating device-role", "name", name)
	payload := map[string]any{
		"name":  name,
		"slug":  slugify(name),
		"color": "9e9e9e", // neutral grey
	}
	body, err = c.post("/api/dcim/device-roles/", payload)
	if err != nil {
		return nil, fmt.Errorf("create device-role %q: %w", name, err)
	}
	if c.DryRun {
		return &DeviceRole{Name: name}, nil
	}
	var r DeviceRole
	return &r, json.Unmarshal(body, &r)
}

// FindSite looks up an existing site by name or slug. Sites are not auto-created.
func (c *Client) FindSite(nameOrSlug string) (*Site, error) {
	for _, key := range []string{"name", "slug"} {
		q := url.Values{key: {nameOrSlug}}
		body, err := c.get("/api/dcim/sites/", q)
		if err != nil {
			return nil, err
		}
		var result listResponse[Site]
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, err
		}
		if result.Count > 0 {
			s := result.Results[0]
			return &s, nil
		}
	}
	return nil, fmt.Errorf("site %q not found in NetBox; create it first", nameOrSlug)
}

// FindOrCreateDevice looks up a device by serial number (then by name) and
// creates or updates it as needed.
func (c *Client) FindOrCreateDevice(
	hostname, serial string,
	deviceTypeID, roleID, siteID int,
) (*Device, error) {
	if serial != "" {
		q := url.Values{"serial": {serial}}
		body, err := c.get("/api/dcim/devices/", q)
		if err != nil {
			return nil, err
		}
		var result listResponse[Device]
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, err
		}
		if result.Count > 0 {
			dev := result.Results[0]
			c.Log.Info("found existing device by serial", "name", dev.Name, "id", dev.ID)
			return c.updateDevice(&dev, hostname, serial, deviceTypeID, roleID, siteID)
		}
	}
	if hostname != "" {
		q := url.Values{"name": {hostname}}
		body, err := c.get("/api/dcim/devices/", q)
		if err != nil {
			return nil, err
		}
		var result listResponse[Device]
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, err
		}
		if result.Count > 0 {
			dev := result.Results[0]
			c.Log.Info("found existing device by name", "name", dev.Name, "id", dev.ID)
			return c.updateDevice(&dev, hostname, serial, deviceTypeID, roleID, siteID)
		}
	}

	c.Log.Info("creating device", "hostname", hostname, "serial", serial)
	payload := map[string]any{
		"name":        hostname,
		"serial":      serial,
		"device_type": deviceTypeID,
		"role":        roleID,
		"site":        siteID,
		"status":      "active",
	}
	body, err := c.post("/api/dcim/devices/", payload)
	if err != nil {
		return nil, fmt.Errorf("create device %q: %w", hostname, err)
	}
	if c.DryRun {
		return &Device{Name: hostname, Serial: serial}, nil
	}
	var dev Device
	return &dev, json.Unmarshal(body, &dev)
}

func (c *Client) updateDevice(
	dev *Device, hostname, serial string,
	deviceTypeID, roleID, siteID int,
) (*Device, error) {
	c.Log.Info("updating device", "id", dev.ID, "hostname", hostname)
	payload := map[string]any{
		"name":        hostname,
		"serial":      serial,
		"device_type": deviceTypeID,
		"role":        roleID,
		"site":        siteID,
	}
	body, err := c.patch(fmt.Sprintf("/api/dcim/devices/%d/", dev.ID), payload)
	if err != nil {
		return nil, fmt.Errorf("update device %d: %w", dev.ID, err)
	}
	if c.DryRun {
		return dev, nil
	}
	var updated Device
	return &updated, json.Unmarshal(body, &updated)
}

// SetPrimaryIP assigns a primary IPv4 or IPv6 address to a device.
func (c *Client) SetPrimaryIP(deviceID, ipID, family int) error {
	key := "primary_ip4"
	if family == 6 {
		key = "primary_ip6"
	}
	_, err := c.patch(fmt.Sprintf("/api/dcim/devices/%d/", deviceID),
		map[string]any{key: ipID})
	return err
}

// FindOrCreateInterface returns an existing interface on a device or creates it.
func (c *Client) FindOrCreateInterface(
	deviceID int,
	name, ifType, description, macAddress string,
	mtu int,
	enabled bool,
) (*Interface, error) {
	q := url.Values{
		"device_id": {fmt.Sprint(deviceID)},
		"name":      {name},
	}
	body, err := c.get("/api/dcim/interfaces/", q)
	if err != nil {
		return nil, err
	}
	var result listResponse[Interface]
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	payload := map[string]any{
		"device":      deviceID,
		"name":        name,
		"type":        ifType,
		"description": description,
		"enabled":     enabled,
	}
	if macAddress != "" {
		payload["mac_address"] = macAddress
	}
	if mtu > 0 {
		payload["mtu"] = mtu
	}

	if result.Count > 0 {
		iface := result.Results[0]
		c.Log.Debug("updating interface", "name", name, "id", iface.ID)
		body, err = c.patch(fmt.Sprintf("/api/dcim/interfaces/%d/", iface.ID), payload)
		if err != nil {
			return nil, fmt.Errorf("update interface %q: %w", name, err)
		}
		if c.DryRun {
			return &iface, nil
		}
		var updated Interface
		return &updated, json.Unmarshal(body, &updated)
	}

	c.Log.Info("creating interface", "device_id", deviceID, "name", name)
	body, err = c.post("/api/dcim/interfaces/", payload)
	if err != nil {
		return nil, fmt.Errorf("create interface %q: %w", name, err)
	}
	if c.DryRun {
		return &Interface{Name: name}, nil
	}
	var iface Interface
	return &iface, json.Unmarshal(body, &iface)
}

// FindOrCreateIPAddress returns an existing IP address or creates it, then
// assigns it to the specified interface.
func (c *Client) FindOrCreateIPAddress(
	cidr, description string,
	family, ifaceID int,
) (*IPAddress, error) {
	q := url.Values{"address": {cidr}}
	body, err := c.get("/api/ipam/ip-addresses/", q)
	if err != nil {
		return nil, err
	}
	var result listResponse[IPAddress]
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	objType := "dcim.interface"
	ifaceIDPtr := &ifaceID

	payload := map[string]any{
		"address":              cidr,
		"description":          description,
		"assigned_object_type": objType,
		"assigned_object_id":   ifaceID,
	}

	if result.Count > 0 {
		ip := result.Results[0]
		c.Log.Debug("updating ip-address", "address", cidr, "id", ip.ID)
		body, err = c.patch(fmt.Sprintf("/api/ipam/ip-addresses/%d/", ip.ID), payload)
		if err != nil {
			return nil, fmt.Errorf("update ip-address %q: %w", cidr, err)
		}
		if c.DryRun {
			ip.AssignedObjectType = objType
			ip.AssignedObjectID = ifaceIDPtr
			return &ip, nil
		}
		var updated IPAddress
		return &updated, json.Unmarshal(body, &updated)
	}

	c.Log.Info("creating ip-address", "address", cidr, "interface_id", ifaceID)
	body, err = c.post("/api/ipam/ip-addresses/", payload)
	if err != nil {
		return nil, fmt.Errorf("create ip-address %q: %w", cidr, err)
	}
	if c.DryRun {
		return &IPAddress{
			Address:              cidr,
			AssignedObjectType:   objType,
			AssignedObjectID:     ifaceIDPtr,
		}, nil
	}
	var ip IPAddress
	return &ip, json.Unmarshal(body, &ip)
}

var reSlugBad = regexp.MustCompile(`[^a-z0-9]+`)

// slugify converts a string to a NetBox-compatible URL slug.
func slugify(s string) string {
	s = strings.ToLower(s)
	s = reSlugBad.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}
