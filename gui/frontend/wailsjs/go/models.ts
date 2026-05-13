export namespace main {
	
	export class ConfigResult {
	    error?: string;
	    server_ip: string;
	    port: number;
	    short_id: number;
	    secret_key: string;
	    internal_ip: string;
	    routing_salt: string;
	    gateway_ip: string;
	    dns: string;
	
	    static createFrom(source: any = {}) {
	        return new ConfigResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.error = source["error"];
	        this.server_ip = source["server_ip"];
	        this.port = source["port"];
	        this.short_id = source["short_id"];
	        this.secret_key = source["secret_key"];
	        this.internal_ip = source["internal_ip"];
	        this.routing_salt = source["routing_salt"];
	        this.gateway_ip = source["gateway_ip"];
	        this.dns = source["dns"];
	    }
	}

}

