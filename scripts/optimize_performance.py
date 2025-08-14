#!/usr/bin/env python3
"""
PocWhisp Performance Optimization Script
Automated performance analysis and optimization for production deployments
"""

import argparse
import json
import os
import requests
import sys
import time
from datetime import datetime, timedelta
from typing import Dict, List, Optional

import psutil
import yaml


class PerformanceOptimizer:
    """Main performance optimizer class"""
    
    def __init__(self, api_url: str, auth_token: Optional[str] = None):
        self.api_url = api_url.rstrip('/')
        self.auth_token = auth_token
        self.session = requests.Session()
        
        if auth_token:
            self.session.headers.update({
                'Authorization': f'Bearer {auth_token}'
            })
    
    def analyze_system_performance(self) -> Dict:
        """Analyze current system performance"""
        print("🔍 Analyzing system performance...")
        
        # System metrics
        cpu_percent = psutil.cpu_percent(interval=1)
        memory = psutil.virtual_memory()
        disk = psutil.disk_usage('/')
        
        # Network metrics
        network = psutil.net_io_counters()
        
        # Process metrics
        processes = []
        for proc in psutil.process_iter(['pid', 'name', 'cpu_percent', 'memory_percent']):
            try:
                if proc.info['cpu_percent'] > 5 or proc.info['memory_percent'] > 5:
                    processes.append(proc.info)
            except (psutil.NoSuchProcess, psutil.AccessDenied):
                pass
        
        system_metrics = {
            'cpu': {
                'usage_percent': cpu_percent,
                'count': psutil.cpu_count(),
                'freq': psutil.cpu_freq()._asdict() if psutil.cpu_freq() else None
            },
            'memory': {
                'total': memory.total,
                'available': memory.available,
                'percent': memory.percent,
                'used': memory.used,
                'free': memory.free
            },
            'disk': {
                'total': disk.total,
                'used': disk.used,
                'free': disk.free,
                'percent': disk.percent
            },
            'network': {
                'bytes_sent': network.bytes_sent,
                'bytes_recv': network.bytes_recv,
                'packets_sent': network.packets_sent,
                'packets_recv': network.packets_recv
            },
            'top_processes': sorted(processes, 
                                  key=lambda x: x['cpu_percent'], 
                                  reverse=True)[:10],
            'timestamp': datetime.now().isoformat()
        }
        
        return system_metrics
    
    def get_api_performance_metrics(self) -> Dict:
        """Get performance metrics from the API"""
        print("📊 Retrieving API performance metrics...")
        
        try:
            response = self.session.get(f"{self.api_url}/api/v1/performance/metrics")
            response.raise_for_status()
            return response.json()
        except requests.RequestException as e:
            print(f"❌ Failed to get API metrics: {e}")
            return {}
    
    def get_system_health(self) -> Dict:
        """Get system health from the API"""
        print("🏥 Checking system health...")
        
        try:
            response = self.session.get(f"{self.api_url}/api/v1/performance/health")
            response.raise_for_status()
            return response.json()
        except requests.RequestException as e:
            print(f"❌ Failed to get system health: {e}")
            return {}
    
    def trigger_optimization(self, optimization_type: str = "all") -> Dict:
        """Trigger performance optimizations"""
        print(f"⚡ Triggering {optimization_type} optimization...")
        
        try:
            response = self.session.post(
                f"{self.api_url}/api/v1/performance/optimize",
                params={'type': optimization_type}
            )
            response.raise_for_status()
            return response.json()
        except requests.RequestException as e:
            print(f"❌ Failed to trigger optimization: {e}")
            return {}
    
    def generate_performance_report(self, period: str = "24h") -> Dict:
        """Generate comprehensive performance report"""
        print(f"📈 Generating performance report for {period}...")
        
        try:
            response = self.session.get(
                f"{self.api_url}/api/v1/performance/report",
                params={'period': period, 'format': 'json'}
            )
            response.raise_for_status()
            return response.json()
        except requests.RequestException as e:
            print(f"❌ Failed to generate report: {e}")
            return {}
    
    def analyze_and_recommend(self) -> Dict:
        """Perform comprehensive analysis and generate recommendations"""
        print("🎯 Performing comprehensive performance analysis...")
        
        # Collect all metrics
        system_metrics = self.analyze_system_performance()
        api_metrics = self.get_api_performance_metrics()
        health_status = self.get_system_health()
        
        # Analyze and generate recommendations
        recommendations = []
        critical_issues = []
        warnings = []
        
        # CPU Analysis
        if system_metrics['cpu']['usage_percent'] > 90:
            critical_issues.append("CPU usage is critically high (>90%)")
            recommendations.append("Consider horizontal scaling or CPU optimization")
        elif system_metrics['cpu']['usage_percent'] > 80:
            warnings.append("CPU usage is high (>80%)")
            recommendations.append("Monitor CPU usage and consider scaling")
        
        # Memory Analysis
        if system_metrics['memory']['percent'] > 95:
            critical_issues.append("Memory usage is critically high (>95%)")
            recommendations.append("Increase memory or optimize memory usage")
        elif system_metrics['memory']['percent'] > 85:
            warnings.append("Memory usage is high (>85%)")
            recommendations.append("Monitor memory usage patterns")
        
        # Disk Analysis
        if system_metrics['disk']['percent'] > 95:
            critical_issues.append("Disk usage is critically high (>95%)")
            recommendations.append("Free up disk space or add storage")
        elif system_metrics['disk']['percent'] > 85:
            warnings.append("Disk usage is high (>85%)")
            recommendations.append("Monitor disk usage and plan for expansion")
        
        # API-specific analysis
        if api_metrics.get('data'):
            api_data = api_metrics['data']
            
            # Check profiler metrics if available
            if 'profiler' in api_data:
                profiler_data = api_data['profiler']
                if 'metrics' in profiler_data:
                    metrics = profiler_data['metrics']
                    
                    # Goroutine analysis
                    if metrics.get('goroutine_count', 0) > 10000:
                        warnings.append("High goroutine count detected")
                        recommendations.append("Check for goroutine leaks")
                    
                    # GC analysis
                    if metrics.get('gc_pause_avg_ns', 0) > 10_000_000:  # 10ms
                        warnings.append("High GC pause times")
                        recommendations.append("Tune garbage collection parameters")
        
        # Overall assessment
        overall_status = "healthy"
        if critical_issues:
            overall_status = "critical"
        elif warnings:
            overall_status = "warning"
        
        return {
            'overall_status': overall_status,
            'system_metrics': system_metrics,
            'api_metrics': api_metrics,
            'health_status': health_status,
            'critical_issues': critical_issues,
            'warnings': warnings,
            'recommendations': recommendations,
            'analysis_timestamp': datetime.now().isoformat()
        }
    
    def apply_optimizations(self, auto_apply: bool = False) -> Dict:
        """Apply performance optimizations based on analysis"""
        print("🔧 Applying performance optimizations...")
        
        results = {}
        
        if auto_apply:
            # Apply all optimizations
            opt_result = self.trigger_optimization("all")
            results['optimization'] = opt_result
        else:
            # Interactive optimization
            print("\nAvailable optimizations:")
            print("1. Garbage Collection (gc)")
            print("2. Cache Optimization (cache)")
            print("3. Connection Pool (connections)")
            print("4. All Optimizations (all)")
            
            choice = input("Select optimization (1-4): ").strip()
            
            opt_type_map = {
                '1': 'gc',
                '2': 'cache',
                '3': 'connections',
                '4': 'all'
            }
            
            opt_type = opt_type_map.get(choice, 'all')
            opt_result = self.trigger_optimization(opt_type)
            results['optimization'] = opt_result
        
        return results
    
    def monitor_performance(self, duration: int = 300, interval: int = 30):
        """Monitor performance over time"""
        print(f"📊 Monitoring performance for {duration} seconds (interval: {interval}s)...")
        
        start_time = time.time()
        measurements = []
        
        try:
            while time.time() - start_time < duration:
                timestamp = datetime.now()
                
                # Collect metrics
                system_metrics = self.analyze_system_performance()
                api_metrics = self.get_api_performance_metrics()
                
                measurement = {
                    'timestamp': timestamp.isoformat(),
                    'system': {
                        'cpu_percent': system_metrics['cpu']['usage_percent'],
                        'memory_percent': system_metrics['memory']['percent'],
                        'disk_percent': system_metrics['disk']['percent']
                    }
                }
                
                # Add API metrics if available
                if api_metrics.get('data'):
                    api_data = api_metrics['data']
                    if 'profiler' in api_data and 'metrics' in api_data['profiler']:
                        api_metrics_data = api_data['profiler']['metrics']
                        measurement['api'] = {
                            'goroutines': api_metrics_data.get('goroutine_count', 0),
                            'heap_alloc': api_metrics_data.get('heap_alloc', 0),
                            'gc_count': api_metrics_data.get('gc_count', 0)
                        }
                
                measurements.append(measurement)
                
                # Print current status
                print(f"[{timestamp.strftime('%H:%M:%S')}] "
                      f"CPU: {system_metrics['cpu']['usage_percent']:.1f}% | "
                      f"Memory: {system_metrics['memory']['percent']:.1f}% | "
                      f"Disk: {system_metrics['disk']['percent']:.1f}%")
                
                time.sleep(interval)
                
        except KeyboardInterrupt:
            print("\n⏹️  Monitoring stopped by user")
        
        return measurements
    
    def save_report(self, data: Dict, filename: str = None):
        """Save analysis report to file"""
        if filename is None:
            timestamp = datetime.now().strftime("%Y%m%d_%H%M%S")
            filename = f"performance_report_{timestamp}.json"
        
        with open(filename, 'w') as f:
            json.dump(data, f, indent=2, default=str)
        
        print(f"💾 Report saved to {filename}")
        return filename


def main():
    parser = argparse.ArgumentParser(description="PocWhisp Performance Optimizer")
    parser.add_argument("--api-url", default="http://localhost:8080",
                       help="API URL (default: http://localhost:8080)")
    parser.add_argument("--auth-token", help="Authentication token")
    parser.add_argument("--mode", choices=["analyze", "optimize", "monitor", "report"],
                       default="analyze", help="Operation mode")
    parser.add_argument("--auto-apply", action="store_true",
                       help="Automatically apply optimizations")
    parser.add_argument("--duration", type=int, default=300,
                       help="Monitoring duration in seconds (default: 300)")
    parser.add_argument("--interval", type=int, default=30,
                       help="Monitoring interval in seconds (default: 30)")
    parser.add_argument("--period", default="24h",
                       choices=["1h", "24h", "7d", "30d"],
                       help="Report period (default: 24h)")
    parser.add_argument("--output", help="Output file for reports")
    parser.add_argument("--config", help="Configuration file")
    
    args = parser.parse_args()
    
    # Load configuration if provided
    config = {}
    if args.config and os.path.exists(args.config):
        with open(args.config, 'r') as f:
            if args.config.endswith('.yaml') or args.config.endswith('.yml'):
                config = yaml.safe_load(f)
            else:
                config = json.load(f)
    
    # Override with command line arguments
    api_url = config.get('api_url', args.api_url)
    auth_token = config.get('auth_token', args.auth_token)
    
    print("🚀 PocWhisp Performance Optimizer")
    print(f"API URL: {api_url}")
    print(f"Mode: {args.mode}")
    print("-" * 50)
    
    optimizer = PerformanceOptimizer(api_url, auth_token)
    
    try:
        if args.mode == "analyze":
            analysis = optimizer.analyze_and_recommend()
            
            print(f"\n📋 Performance Analysis Results")
            print(f"Overall Status: {analysis['overall_status'].upper()}")
            
            if analysis['critical_issues']:
                print(f"\n🚨 Critical Issues ({len(analysis['critical_issues'])}):")
                for issue in analysis['critical_issues']:
                    print(f"  • {issue}")
            
            if analysis['warnings']:
                print(f"\n⚠️  Warnings ({len(analysis['warnings'])}):")
                for warning in analysis['warnings']:
                    print(f"  • {warning}")
            
            if analysis['recommendations']:
                print(f"\n💡 Recommendations ({len(analysis['recommendations'])}):")
                for rec in analysis['recommendations']:
                    print(f"  • {rec}")
            
            if args.output:
                optimizer.save_report(analysis, args.output)
        
        elif args.mode == "optimize":
            optimizations = optimizer.apply_optimizations(args.auto_apply)
            
            print("\n✅ Optimization Results:")
            if 'optimization' in optimizations:
                opt_data = optimizations['optimization']
                if opt_data.get('status') == 'success':
                    print("  • Optimizations applied successfully")
                    data = opt_data.get('data', {})
                    if 'optimizations_applied' in data:
                        for opt in data['optimizations_applied']:
                            print(f"    - {opt}")
                else:
                    print("  • Optimization failed")
            
            if args.output:
                optimizer.save_report(optimizations, args.output)
        
        elif args.mode == "monitor":
            measurements = optimizer.monitor_performance(args.duration, args.interval)
            
            print(f"\n📊 Monitoring completed. Collected {len(measurements)} measurements.")
            
            if args.output:
                optimizer.save_report({'measurements': measurements}, args.output)
        
        elif args.mode == "report":
            report = optimizer.generate_performance_report(args.period)
            
            print(f"\n📈 Performance Report Generated")
            if report.get('status') == 'success':
                print("  • Report generated successfully")
                print(f"  • Period: {args.period}")
                print(f"  • Generated at: {report.get('data', {}).get('generated_at', 'N/A')}")
            else:
                print("  • Report generation failed")
            
            if args.output:
                optimizer.save_report(report, args.output)
    
    except KeyboardInterrupt:
        print("\n⏹️  Operation cancelled by user")
        sys.exit(1)
    except Exception as e:
        print(f"\n❌ Error: {e}")
        sys.exit(1)


if __name__ == "__main__":
    main()
