package provisioner

// EnsureCacheZone: nginx fastcgi_cache 'girgincache' zone'unun http bağlamında
// tanımlı olmasını sağlar (idempotent). Alt alan fastcgi_cache'i için dışa açık.
func EnsureCacheZone() (bool, error) { return ensureCacheZone() }
