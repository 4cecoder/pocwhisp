#!/usr/bin/env python3
"""
Comprehensive Test Runner for PocWhisp API
Orchestrates unit tests, integration tests, and load tests with detailed reporting
"""

import argparse
import json
import os
import subprocess
import sys
import time
from datetime import datetime
from pathlib import Path
from typing import Dict, List, Optional


class Colors:
    """ANSI color codes for terminal output"""
    HEADER = '\033[95m'
    OKBLUE = '\033[94m'
    OKCYAN = '\033[96m'
    OKGREEN = '\033[92m'
    WARNING = '\033[93m'
    FAIL = '\033[91m'
    ENDC = '\033[0m'
    BOLD = '\033[1m'
    UNDERLINE = '\033[4m'


class TestResult:
    """Represents the result of a test execution"""
    
    def __init__(self, name: str, passed: bool, duration: float, output: str = "", error: str = ""):
        self.name = name
        self.passed = passed
        self.duration = duration
        self.output = output
        self.error = error


class TestRunner:
    """Main test runner class"""
    
    def __init__(self, project_root: Path):
        self.project_root = project_root
        self.api_root = project_root / "api"
        self.tests_root = project_root / "tests"
        self.results: List[TestResult] = []
        self.start_time = datetime.now()
        
    def print_header(self, text: str):
        """Print a colored header"""
        print(f"\n{Colors.HEADER}{Colors.BOLD}{'='*60}{Colors.ENDC}")
        print(f"{Colors.HEADER}{Colors.BOLD}{text.center(60)}{Colors.ENDC}")
        print(f"{Colors.HEADER}{Colors.BOLD}{'='*60}{Colors.ENDC}\n")
    
    def print_success(self, text: str):
        """Print success message"""
        print(f"{Colors.OKGREEN}✓ {text}{Colors.ENDC}")
    
    def print_error(self, text: str):
        """Print error message"""
        print(f"{Colors.FAIL}✗ {text}{Colors.ENDC}")
    
    def print_warning(self, text: str):
        """Print warning message"""
        print(f"{Colors.WARNING}⚠ {text}{Colors.ENDC}")
    
    def print_info(self, text: str):
        """Print info message"""
        print(f"{Colors.OKCYAN}ℹ {text}{Colors.ENDC}")
    
    def run_command(self, cmd: List[str], cwd: Path, timeout: int = 300) -> TestResult:
        """Run a command and return the result"""
        start_time = time.time()
        test_name = " ".join(cmd)
        
        try:
            self.print_info(f"Running: {test_name}")
            
            result = subprocess.run(
                cmd,
                cwd=cwd,
                capture_output=True,
                text=True,
                timeout=timeout
            )
            
            duration = time.time() - start_time
            
            if result.returncode == 0:
                self.print_success(f"Passed: {test_name} ({duration:.2f}s)")
                return TestResult(test_name, True, duration, result.stdout, result.stderr)
            else:
                self.print_error(f"Failed: {test_name} ({duration:.2f}s)")
                print(f"  Error: {result.stderr}")
                return TestResult(test_name, False, duration, result.stdout, result.stderr)
                
        except subprocess.TimeoutExpired:
            duration = time.time() - start_time
            self.print_error(f"Timeout: {test_name} ({duration:.2f}s)")
            return TestResult(test_name, False, duration, "", "Test timed out")
        
        except Exception as e:
            duration = time.time() - start_time
            self.print_error(f"Exception: {test_name} ({duration:.2f}s)")
            return TestResult(test_name, False, duration, "", str(e))
    
    def check_dependencies(self) -> bool:
        """Check if required dependencies are available"""
        self.print_header("Checking Dependencies")
        
        dependencies = [
            ("go", ["go", "version"]),
            ("python", ["python3", "--version"]),
            ("locust", ["locust", "--version"]),
        ]
        
        all_good = True
        for name, cmd in dependencies:
            try:
                result = subprocess.run(cmd, capture_output=True, text=True)
                if result.returncode == 0:
                    version = result.stdout.strip()
                    self.print_success(f"{name}: {version}")
                else:
                    self.print_error(f"{name}: Not found or error")
                    all_good = False
            except FileNotFoundError:
                self.print_error(f"{name}: Not found in PATH")
                all_good = False
        
        return all_good
    
    def run_go_unit_tests(self) -> List[TestResult]:
        """Run Go unit tests"""
        self.print_header("Running Go Unit Tests")
        
        results = []
        
        # Run tests for each package
        packages = [
            "models",
            "utils", 
            "services",
            "handlers",
            "middleware",
        ]
        
        for package in packages:
            package_path = self.api_root / package
            if package_path.exists():
                # Run unit tests with coverage
                cmd = ["go", "test", "-v", "-race", "-cover", f"./{package}"]
                result = self.run_command(cmd, self.api_root)
                results.append(result)
        
        # Run overall test coverage
        cmd = ["go", "test", "-v", "-race", "-coverprofile=coverage.out", "./..."]
        result = self.run_command(cmd, self.api_root)
        results.append(result)
        
        # Generate coverage report
        if (self.api_root / "coverage.out").exists():
            cmd = ["go", "tool", "cover", "-html=coverage.out", "-o", "coverage.html"]
            result = self.run_command(cmd, self.api_root)
            results.append(result)
            
            cmd = ["go", "tool", "cover", "-func=coverage.out"]
            result = self.run_command(cmd, self.api_root)
            results.append(result)
        
        return results
    
    def run_go_integration_tests(self) -> List[TestResult]:
        """Run Go integration tests"""
        self.print_header("Running Go Integration Tests")
        
        results = []
        
        # Run integration tests in the tests directory
        if (self.api_root / "tests").exists():
            cmd = ["go", "test", "-v", "-race", "-tags=integration", "./tests"]
            result = self.run_command(cmd, self.api_root, timeout=600)  # Longer timeout
            results.append(result)
        
        return results
    
    def run_python_tests(self) -> List[TestResult]:
        """Run Python integration and API tests"""
        self.print_header("Running Python Integration Tests")
        
        results = []
        
        # List of Python test scripts
        python_tests = [
            "test_api.py",
            "integration_test.py",
            "websocket_test.py",
            "queue_test.py",
            "cache_test.py",
            "batch_test.py",
            "swagger_test.py",
        ]
        
        for test_file in python_tests:
            test_path = self.tests_root / test_file
            if test_path.exists():
                cmd = ["python3", str(test_path)]
                result = self.run_command(cmd, self.tests_root)
                results.append(result)
        
        return results
    
    def run_load_tests(self, users: int = 10, spawn_rate: int = 2, duration: int = 60) -> List[TestResult]:
        """Run load tests using Locust"""
        self.print_header("Running Load Tests")
        
        results = []
        
        # Check if load test file exists
        load_test_file = self.tests_root / "load_test.py"
        if not load_test_file.exists():
            self.print_error("Load test file not found")
            return results
        
        # Run light load test
        self.print_info(f"Running load test: {users} users, {spawn_rate}/s spawn rate, {duration}s duration")
        
        cmd = [
            "locust",
            "-f", str(load_test_file),
            "--host", "http://localhost:8080",
            "--users", str(users),
            "--spawn-rate", str(spawn_rate),
            "--run-time", f"{duration}s",
            "--headless",
            "--html", str(self.tests_root / "load_test_report.html"),
            "--csv", str(self.tests_root / "load_test_results")
        ]
        
        result = self.run_command(cmd, self.tests_root, timeout=duration + 30)
        results.append(result)
        
        return results
    
    def run_benchmark_tests(self) -> List[TestResult]:
        """Run Go benchmark tests"""
        self.print_header("Running Benchmark Tests")
        
        results = []
        
        # Run benchmarks for each package
        packages = ["models", "utils", "services"]
        
        for package in packages:
            package_path = self.api_root / package
            if package_path.exists():
                cmd = ["go", "test", "-bench=.", "-benchmem", f"./{package}"]
                result = self.run_command(cmd, self.api_root)
                results.append(result)
        
        return results
    
    def run_security_tests(self) -> List[TestResult]:
        """Run security tests"""
        self.print_header("Running Security Tests")
        
        results = []
        
        # Run gosec security scanner
        cmd = ["gosec", "./..."]
        result = self.run_command(cmd, self.api_root)
        results.append(result)
        
        return results
    
    def generate_report(self):
        """Generate comprehensive test report"""
        self.print_header("Test Report")
        
        total_tests = len(self.results)
        passed_tests = sum(1 for r in self.results if r.passed)
        failed_tests = total_tests - passed_tests
        total_duration = sum(r.duration for r in self.results)
        
        print(f"Test Execution Summary:")
        print(f"  Total Tests: {total_tests}")
        print(f"  Passed: {Colors.OKGREEN}{passed_tests}{Colors.ENDC}")
        print(f"  Failed: {Colors.FAIL}{failed_tests}{Colors.ENDC}")
        print(f"  Success Rate: {(passed_tests/total_tests*100):.1f}%" if total_tests > 0 else "  Success Rate: N/A")
        print(f"  Total Duration: {total_duration:.2f}s")
        print(f"  Start Time: {self.start_time}")
        print(f"  End Time: {datetime.now()}")
        
        if failed_tests > 0:
            print(f"\n{Colors.FAIL}Failed Tests:{Colors.ENDC}")
            for result in self.results:
                if not result.passed:
                    print(f"  ✗ {result.name}")
                    if result.error:
                        print(f"    Error: {result.error}")
        
        # Generate JSON report
        report_data = {
            "summary": {
                "total_tests": total_tests,
                "passed_tests": passed_tests,
                "failed_tests": failed_tests,
                "success_rate": (passed_tests/total_tests*100) if total_tests > 0 else 0,
                "total_duration": total_duration,
                "start_time": self.start_time.isoformat(),
                "end_time": datetime.now().isoformat()
            },
            "results": [
                {
                    "name": r.name,
                    "passed": r.passed,
                    "duration": r.duration,
                    "error": r.error
                }
                for r in self.results
            ]
        }
        
        report_file = self.tests_root / "test_report.json"
        with open(report_file, 'w') as f:
            json.dump(report_data, f, indent=2)
        
        self.print_info(f"Detailed report saved to: {report_file}")
        
        return failed_tests == 0
    
    def run_all_tests(self, include_load: bool = True, include_security: bool = True, 
                     load_users: int = 10, load_duration: int = 60):
        """Run all test suites"""
        self.print_header("PocWhisp API Test Suite")
        self.print_info(f"Project root: {self.project_root}")
        self.print_info(f"API root: {self.api_root}")
        self.print_info(f"Tests root: {self.tests_root}")
        
        # Check dependencies
        if not self.check_dependencies():
            self.print_error("Missing dependencies. Please install required tools.")
            return False
        
        # Run test suites
        self.results.extend(self.run_go_unit_tests())
        self.results.extend(self.run_go_integration_tests())
        self.results.extend(self.run_python_tests())
        self.results.extend(self.run_benchmark_tests())
        
        if include_security:
            self.results.extend(self.run_security_tests())
        
        if include_load:
            self.results.extend(self.run_load_tests(
                users=load_users, 
                duration=load_duration
            ))
        
        # Generate report
        return self.generate_report()


def main():
    """Main entry point"""
    parser = argparse.ArgumentParser(description="PocWhisp API Test Runner")
    parser.add_argument("--no-load", action="store_true", help="Skip load tests")
    parser.add_argument("--no-security", action="store_true", help="Skip security tests")
    parser.add_argument("--load-users", type=int, default=10, help="Number of users for load test")
    parser.add_argument("--load-duration", type=int, default=60, help="Load test duration in seconds")
    parser.add_argument("--unit-only", action="store_true", help="Run only unit tests")
    parser.add_argument("--integration-only", action="store_true", help="Run only integration tests")
    parser.add_argument("--load-only", action="store_true", help="Run only load tests")
    parser.add_argument("--benchmark-only", action="store_true", help="Run only benchmark tests")
    
    args = parser.parse_args()
    
    # Find project root
    current_dir = Path(__file__).parent
    project_root = current_dir.parent
    
    runner = TestRunner(project_root)
    
    success = True
    
    if args.unit_only:
        runner.results.extend(runner.run_go_unit_tests())
    elif args.integration_only:
        runner.results.extend(runner.run_go_integration_tests())
        runner.results.extend(runner.run_python_tests())
    elif args.load_only:
        runner.results.extend(runner.run_load_tests(
            users=args.load_users,
            duration=args.load_duration
        ))
    elif args.benchmark_only:
        runner.results.extend(runner.run_benchmark_tests())
    else:
        success = runner.run_all_tests(
            include_load=not args.no_load,
            include_security=not args.no_security,
            load_users=args.load_users,
            load_duration=args.load_duration
        )
    
    if not success:
        runner.print_error("Some tests failed!")
        sys.exit(1)
    else:
        runner.print_success("All tests passed!")


if __name__ == "__main__":
    main()
