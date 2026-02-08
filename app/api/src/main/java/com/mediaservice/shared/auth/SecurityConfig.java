package com.mediaservice.shared.auth;

import lombok.RequiredArgsConstructor;
import org.springframework.boot.web.servlet.FilterRegistrationBean;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;
import org.springframework.security.config.annotation.web.builders.HttpSecurity;
import org.springframework.security.config.annotation.web.configuration.EnableWebSecurity;
import org.springframework.security.config.http.SessionCreationPolicy;
import org.springframework.security.crypto.bcrypt.BCryptPasswordEncoder;
import org.springframework.security.crypto.password.PasswordEncoder;
import org.springframework.security.web.SecurityFilterChain;
import org.springframework.security.web.authentication.UsernamePasswordAuthenticationFilter;

@Configuration
@EnableWebSecurity
@RequiredArgsConstructor
public class SecurityConfig {
  private final AuthenticationFilter authenticationFilter;
  private final AuthProperties authProperties;

  @Bean
  public SecurityFilterChain securityFilterChain(HttpSecurity http) throws Exception {
    http
        .csrf(csrf -> csrf.disable())
        .sessionManagement(session -> session.sessionCreationPolicy(SessionCreationPolicy.STATELESS))
        .addFilterBefore(authenticationFilter, UsernamePasswordAuthenticationFilter.class);

    if (authProperties.getEnforcement().isEnabled()) {
      http.authorizeHttpRequests(auth -> auth
          .requestMatchers("/actuator/**", "/swagger-ui/**", "/v3/api-docs/**").permitAll()
          .requestMatchers("/v1/media/health").permitAll()
          .requestMatchers("/v1/auth/**").permitAll()
          .requestMatchers(org.springframework.http.HttpMethod.GET, "/v1/media/*/preview").permitAll()
          .requestMatchers("/admin/**").hasRole("ADMIN")
          .anyRequest().authenticated());
    } else {
      http.authorizeHttpRequests(auth -> auth.anyRequest().permitAll());
    }

    return http.build();
  }

  /**
   * Disable servlet container auto-registration for AuthenticationFilter.
   * We only want it to run inside Spring Security's filter chain.
   */
  @Bean
  public FilterRegistrationBean<AuthenticationFilter> authenticationFilterRegistration(
      AuthenticationFilter filter) {
    FilterRegistrationBean<AuthenticationFilter> registration = new FilterRegistrationBean<>(filter);
    registration.setEnabled(false);
    return registration;
  }

  @Bean
  public PasswordEncoder passwordEncoder() {
    return new BCryptPasswordEncoder();
  }
}
